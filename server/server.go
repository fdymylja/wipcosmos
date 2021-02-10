package server

import (
	"context"
	"fmt"
	sdkcrypto "github.com/cosmos/cosmos-sdk/crypto/types"
	"github.com/cosmos/cosmos-sdk/simapp"
	sdkcodec "github.com/cosmos/cosmos-sdk/simapp/params"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
	auth "github.com/cosmos/cosmos-sdk/x/auth/types"
	bank "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/tendermint/tendermint/abci/types"
	"github.com/tendermint/tendermint/rpc/client/http"
	"google.golang.org/grpc"
)

const tmWebsocketPath = "/websocket"

type BankSendRequest struct {
	Sender   string `json:"sender"`
	Receiver string `json:"receiver"`
	Amount   string `json:"amount"`
	FeeCoin  string `json:"fee_coin"`
	GasLimit uint64 `json:"gas_limit"`
}

type BfxClient struct {
	chainID string

	auth auth.QueryClient
	bank bank.QueryClient
	tm   *http.HTTP

	encoding sdkcodec.EncodingConfig

	resolver PrivateKeyResolver
}

type PrivateKeyResolver interface {
	Resolve(addr []byte) (sdkcrypto.PrivKey, error)
}

func NewBfxClient(resolver PrivateKeyResolver, chainID, grpcEndpoint, tmEndpoint string) (*BfxClient, error) {
	encoding := simapp.MakeTestEncodingConfig()
	grpcConn, err := grpc.Dial(grpcEndpoint, grpc.WithInsecure())
	if err != nil {
		return nil, err
	}

	tmRPC, err := http.New(tmEndpoint, tmWebsocketPath)
	if err != nil {
		return nil, err
	}
	// check tendermint rpc liveness
	_, err = tmRPC.Status(context.TODO())
	if err != nil {
		return nil, err
	}
	bankClient := bank.NewQueryClient(grpcConn)
	// check grpc liveness
	_, err = bankClient.Params(context.TODO(), &bank.QueryParamsRequest{})
	if err != nil {
		return nil, err
	}
	authClient := auth.NewQueryClient(grpcConn)
	return &BfxClient{
		chainID:  chainID,
		auth:     authClient,
		bank:     bankClient,
		tm:       tmRPC,
		encoding: encoding,
		resolver: resolver,
	}, nil
}

func (c *BfxClient) Send(ctx context.Context, req BankSendRequest) (txHash string, err error) {
	// do req verification
	senderAddr, err := sdk.AccAddressFromBech32(req.Sender)
	if err != nil {
		return
	}
	recvAddr, err := sdk.AccAddressFromBech32(req.Receiver)
	if err != nil {
		return
	}

	amount, err := sdk.ParseCoinsNormalized(req.Amount)
	if err != nil {
		return
	}

	feeCoin, err := sdk.ParseCoinsNormalized(req.FeeCoin)
	if err != nil {
		return
	}

	msgBankSend := bank.NewMsgSend(senderAddr, recvAddr, amount)

	// get account
	privKey, err := c.resolver.Resolve(senderAddr.Bytes())
	if err != nil {
		return "", err
	}
	// sign
	txBytes, err := c.sign(ctx, privKey, msgBankSend, feeCoin, req.GasLimit)
	if err != nil {
		return
	}

	// post tx in async mode
	tmResp, err := c.tm.BroadcastTxSync(ctx, txBytes)
	if err != nil {
		return
	}
	if tmResp.Code != types.CodeTypeOK {
		return "", fmt.Errorf("transaction rejected: code(%d): %s", tmResp.Code, tmResp.Log)
	}
	return fmt.Sprintf("%x", tmResp.Hash), nil
}

func (c *BfxClient) sign(ctx context.Context, privKey sdkcrypto.PrivKey, msg sdk.Msg, fee sdk.Coins, gasLimit uint64) (txBytes []byte, err error) {
	// verify if msg is correct
	if err = msg.ValidateBasic(); err != nil {
		return nil, err
	}
	builder := c.encoding.TxConfig.NewTxBuilder()

	// set fees and gas limit
	builder.SetFeeAmount(fee)
	builder.SetGasLimit(gasLimit)

	err = builder.SetMsgs(msg)
	if err != nil {
		return nil, err
	}

	signMode := c.encoding.TxConfig.SignModeHandler().DefaultMode()

	// get account data
	accountRaw, err := c.auth.Account(ctx, &auth.QueryAccountRequest{Address: msg.GetSigners()[0].String()})
	if err != nil {
		return nil, err
	}
	var account auth.AccountI
	err = c.encoding.Marshaler.UnpackAny(accountRaw.Account, &account)
	if err != nil {
		return nil, err
	}

	signerData := authsigning.SignerData{
		ChainID:       c.chainID,
		AccountNumber: account.GetAccountNumber(),
		Sequence:      account.GetSequence(),
	}

	sigData := signing.SingleSignatureData{
		SignMode:  signMode,
		Signature: nil,
	}

	sig := signing.SignatureV2{
		PubKey:   privKey.PubKey(),
		Data:     &sigData,
		Sequence: account.GetSequence(),
	}

	err = builder.SetSignatures(sig)
	if err != nil {
		return nil, err
	}

	unsignedTx, err := c.encoding.TxConfig.SignModeHandler().GetSignBytes(
		signMode,
		signerData,
		builder.GetTx(),
	)
	if err != nil {
		return nil, err
	}

	signedTx, err := privKey.Sign(unsignedTx)
	if err != nil {
		return nil, err
	}

	sigData = signing.SingleSignatureData{
		SignMode:  signMode,
		Signature: signedTx,
	}
	sig = signing.SignatureV2{
		PubKey:   privKey.PubKey(),
		Data:     &sigData,
		Sequence: account.GetSequence(),
	}

	err = builder.SetSignatures(sig)
	if err != nil {
		return nil, err
	}

	txBytes, err = c.encoding.TxConfig.TxEncoder()(builder.GetTx())

	return
}
