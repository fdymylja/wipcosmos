package server

import (
	"bytes"
	"fmt"
	sdkcrypto "github.com/cosmos/cosmos-sdk/crypto/types"
)

type BitfinexResolver struct {
	// add bfx strategy to get private keys from address bytes
}

type singleKeyResolver struct {
	privateKey sdkcrypto.PrivKey
}

func (s singleKeyResolver) Resolve(addr []byte) (sdkcrypto.PrivKey, error) {
	if !bytes.Equal(addr, s.privateKey.PubKey().Address().Bytes()) {
		return nil, fmt.Errorf("address not found")
	}
	return s.privateKey, nil
}
