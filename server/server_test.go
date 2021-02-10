package server

import (
	"context"
	"encoding/hex"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"testing"
)

func TestServer(t *testing.T) {
	privKeyHex := "acfa130eda5904f1d04125fb24c1d8ab9c0fbd223862b3887047b67804f33b14"
	privKeyBytes, err := hex.DecodeString(privKeyHex)
	if err != nil {
		t.Fatal(err)
	}
	privKey := &secp256k1.PrivKey{Key: privKeyBytes}

	resolver := singleKeyResolver{privateKey: privKey}

	accAddr := sdk.AccAddress(privKey.PubKey().Address().Bytes())

	bfx, err := NewBfxClient(resolver, "testing", "localhost:9090", "tcp://localhost:26657")
	if err != nil {
		t.Fatal(err)
	}

	txHash, err := bfx.Send(context.TODO(), BankSendRequest{
		Sender:   accAddr.String(),
		Receiver: sdk.AccAddress(secp256k1.GenPrivKey().PubKey().Address().Bytes()).String(),
		Amount:   "100stake",
		FeeCoin:  "10stake",
		GasLimit: 200000,
	})

	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%s", txHash)
}
