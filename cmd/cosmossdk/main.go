package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/client/tx"
	"github.com/cosmos/cosmos-sdk/crypto/hd"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	"github.com/cosmos/cosmos-sdk/simapp"
	"github.com/cosmos/cosmos-sdk/types"
	signingtypes "github.com/cosmos/cosmos-sdk/types/tx/signing"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/julienschmidt/httprouter"
	"github.com/pkg/errors"
	gouuid "github.com/satori/go.uuid"
	"github.com/wipcosmos/pkg/request"
)

const (
	configPath  = "./config/cosmos.coin.json"
	keystoreDir = "./db"
)

type BankSendBody struct {
	AccountNumber uint64           `json:"accountNumber"`
	Sequence      uint64           `json:"sequence"`
	Sender        types.AccAddress `json:"sender"`
	Receiver      types.AccAddress `json:"receiver"`
	Denom         string           `json:"denom"`
	Amount        int64            `json:"amount"`
	ChainID       string           `json:"chainId"`
	Memo          string           `json:"memo,omitempty"`
	Fee           int64            `json:"fees,omitempty"`
	GasAdjustment float64          `json:"gasAdjustment,omitempty"`
	Gas           uint64           `json:"gas"`
}

type Config struct {
	Provider           string  `json:"provider"`
	TendermintProvider string  `json:"tendermintProvider"`
	Currency           string  `json:"currency"`
	ChainID            string  `json:"chainId"`
	Denom              string  `json:"denom"`
	CosmosSdkPort      string  `json:"cosmosSdkPort"`
	Password           string  `json:"password"`
	Fee                int64   `json:"txFee"`
	GasAdjustment      float64 `json:"txGasAdjustment"`
	Gas                uint64  `json:"txGas"`
}

type CosmosSdkApi struct {
	Config  Config
	keyring keyring.Keyring
}

func loadCoinConfig() Config {
	fmt.Printf("loading config from: %s\n", configPath)
	cfgbytes, _ := ioutil.ReadFile(configPath)
	var cfg Config
	if err := json.Unmarshal(cfgbytes, &cfg); err != nil {
		log.Fatalf("Unable to load cosmos.coin.json: %s", err)
	}
	return cfg
}

func main() {
	kr, err := keyring.New(types.KeyringServiceName(), keyring.BackendOS, keystoreDir, nil)
	if err != nil {
		log.Fatalf("%s", errors.Wrap(err, "keyring.New"))
		return
	}

	if _, err := kr.List(); err != nil {
		log.Fatalf("%s", errors.Wrap(err, "initial authentication failed"))
	}

	config := loadCoinConfig()
	sdk := CosmosSdkApi{
		Config:  config,
		keyring: kr,
	}

	router := httprouter.New()
	router.GET("/address", sdk.CreateAddress)
	router.POST("/send_transaction", sdk.Send)

	fmt.Printf("cosmos SDK REST API started on: %s\n", config.CosmosSdkPort)
	if err := http.ListenAndServe(":"+config.CosmosSdkPort, router); err != nil {
		log.Fatalf("cosmos SDK rest server stopped: %s", err)
	}
}

func (sdk *CosmosSdkApi) CreateAddress(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	fmt.Printf("%s - %s\n", r.Method, r.URL.Path)
	defer r.Body.Close()

	uuid := gouuid.NewV4()
	uids := uuid.String()

	info, _, err := sdk.keyring.NewMnemonic(uids, keyring.English, types.FullFundraiserPath, hd.Secp256k1)
	if err != nil {
		http.Error(w, errors.Wrap(err, "kr.NewMnemonic").Error(), 500)
		return
	}

	var keypair = struct {
		Address string
	}{
		Address: info.GetAddress().String(),
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	if err = json.NewEncoder(w).Encode(keypair); err != nil {
		http.Error(w, errors.Wrap(err, "json.NewEncoder").Error(), 500)
		return
	}
}

func (sdk *CosmosSdkApi) Send(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	fmt.Printf("%s - %s\n", r.Method, r.URL.Path)
	defer r.Body.Close()

	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		log.Printf("send:ioutil.ReadAll:%s\n", err)
		http.Error(w, errors.Wrap(err, "send:ioutil.ReadAll").Error(), 500)
		return
	}

	var bsb BankSendBody
	if err := json.Unmarshal(body, &bsb); err != nil {
		log.Printf("send:json.Unmarshal:%s\n", err)
		http.Error(w, errors.Wrap(err, "send:json.Unmarshal").Error(), 500)
		return
	}
	bsb.Denom = sdk.Config.Denom
	bsb.Gas = sdk.Config.Gas
	bsb.Fee = sdk.Config.Fee
	bsb.GasAdjustment = sdk.Config.GasAdjustment
	bsb.ChainID = sdk.Config.ChainID

	info, err := sdk.keyring.KeyByAddress(bsb.Sender)
	if err != nil {
		log.Printf("send:sdk.keyring.KeyByAddress:%s\n", err)
		http.Error(w, errors.Wrap(err, "send:sdk.keyring.KeyByAddress").Error(), 500)
		return
	}

	coins := types.NewCoins(types.NewInt64Coin(sdk.Config.Denom, bsb.Amount))
	encodingConfig := simapp.MakeTestEncodingConfig()

	log.Printf("from client pld: %+v\n", bsb)

	cfg := client.Context{}.
		WithJSONMarshaler(encodingConfig.Marshaler).
		WithInterfaceRegistry(encodingConfig.InterfaceRegistry).
		WithTxConfig(encodingConfig.TxConfig).
		WithLegacyAmino(encodingConfig.Amino).
		WithInput(os.Stdin).
		WithAccountRetriever(authtypes.AccountRetriever{}).
		WithBroadcastMode(flags.BroadcastBlock).
		WithHomeDir(keystoreDir).
		WithKeyring(sdk.keyring).
		WithSkipConfirmation(true).
		WithNodeURI(sdk.Config.TendermintProvider)

	txfNoKeybase := tx.Factory{}.
		WithTxConfig(encodingConfig.TxConfig).
		WithAccountNumber(bsb.AccountNumber).
		WithSequence(bsb.Sequence).
		WithFees(fmt.Sprintf("%d%s", bsb.Fee, coins.GetDenomByIndex(0))).
		WithMemo(bsb.Memo).
		WithGas(bsb.Gas).
		WithGasAdjustment(bsb.GasAdjustment).
		WithChainID(bsb.ChainID)

	txfDirect := txfNoKeybase.
		WithKeybase(cfg.Keyring).
		WithSignMode(signingtypes.SignMode_SIGN_MODE_DIRECT)

	msg := banktypes.NewMsgSend(info.GetAddress(), bsb.Receiver, coins)

	txb, err := tx.BuildUnsignedTx(txfDirect, msg)
	if err != nil {
		log.Printf("send:tx.BuildUnsignedTx:%s\n", err)
		http.Error(w, errors.Wrap(err, "send:sdk.tx.BuildUnsignedTx").Error(), 500)
		return
	}

	txb.SetMemo(bsb.Memo)

	txf := tx.Factory{}.
		WithTxConfig(cfg.TxConfig).
		WithAccountRetriever(cfg.AccountRetriever).
		WithKeybase(cfg.Keyring).
		WithChainID(cfg.ChainID).
		WithSimulateAndExecute(true).
		WithGasAdjustment(bsb.GasAdjustment).
		WithGasPrices(fmt.Sprintf("%d%s", sdk.Config.Fee, sdk.Config.Denom)).
		WithSignMode(signingtypes.SignMode_SIGN_MODE_DIRECT)

	if err = tx.Sign(txf, info.GetName(), txb, true); err != nil {
		log.Printf("send:tx.Sign:%s\n", err)
		http.Error(w, errors.Wrap(err, "send:sdk.tx.Sign").Error(), 500)
		return
	}

	txBytes, err := cfg.TxConfig.TxEncoder()(txb.GetTx())
	if err != nil {
		log.Printf("send:cfg.TxConfig.TxEncoder:%s\n", err)
		http.Error(w, errors.Wrap(err, "send:cfg.TxConfig.TxEncoder").Error(), 500)
		return
	}

	// grpcConn, err := grpc.Dial(
	// 	sdk.Config.TendermintProvider,
	// 	grpc.WithInsecure(),
	// )
	// if err != nil {
	// 	log.Printf("send:grpc.Dial:%s\n", err)
	// 	http.Error(w, errors.Wrap(err, "send:grpc.Dial").Error(), 500)
	// 	return
	// }
	// defer grpcConn.Close()

	// txClient := txt.NewServiceClient(grpcConn)
	// grpcRes, err := txClient.BroadcastTx(
	// 	r.Context(),
	// 	&txt.BroadcastTxRequest{
	// 		Mode:    txt.BroadcastMode_BROADCAST_MODE_SYNC,
	// 		TxBytes: txBytes,
	// 	},
	// )
	// if err != nil {
	// 	log.Printf("send:txClient.BroadcastTx:%s\n", err)
	// 	http.Error(w, errors.Wrap(err, "send:txClient.BroadcastTx").Error(), 500)
	// 	return
	// }

	var pld = struct {
		TxBytes []byte `json:"tx_bytes"`
		Mode    string `json:"mode"`
	}{
		TxBytes: txBytes,
		Mode:    "BROADCAST_MODE_SYNC",
	}

	pldBytes, err := json.Marshal(pld)
	if err != nil {
		log.Printf("send:json.Marshal:%s\n", err)
		http.Error(w, errors.Wrap(err, "send:json.Marshal").Error(), 500)
		return
	}

	req := request.
		New("POST", fmt.Sprintf("%s/cosmos/tx/v1beta1/txs", sdk.Config.Provider), bytes.NewReader(pldBytes)).
		AddHeaders("Content-Type", "application/json").
		Do().
		Read()

	if err := req.HasError(); err != nil {
		log.Printf("send:req.HasError:%s\n", err)
		http.Error(w, errors.Wrap(err, "send:req.HasError").Error(), 500)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err = json.NewEncoder(w).Encode(req.ResBytes); err != nil {
		log.Printf("send:json.NewEncoder:%s\n", err)
	}
}
