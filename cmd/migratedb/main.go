package main

import (
	"log"
	"os"

	"github.com/cosmos/cosmos-sdk/client/keys"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	"github.com/cosmos/cosmos-sdk/types"
	"github.com/pkg/errors"
)

const keystoreDir = "./db"

func main() {
	if _, err := os.Stat(keystoreDir); os.IsNotExist(err) {
		log.Printf("missing '%s', ensure keystoreDir is set correctly", keystoreDir)
		return
	}

	legacyKb, err := keys.NewLegacyKeyBaseFromDir(keystoreDir)
	if err != nil {
		log.Printf("%s\n", errors.Wrap(err, "keys.NewLegacyKeyBaseFromDir"))
		return
	}
	defer legacyKb.Close()

	oldKeys, err := legacyKb.List()
	if err != nil {
		log.Printf("%s\n", errors.Wrap(err, "legacyKb.List"))
		return
	}

	migrator, err := keyring.NewInfoImporter(types.KeyringServiceName(), keyring.BackendOS, keystoreDir, nil)
	if err != nil {
		log.Printf("%s\n", errors.Wrap(err, "keyring.NewInfoImporter"))
		return
	}

	for _, key := range oldKeys {
		legKeyInfo, err := legacyKb.Export(key.GetName())
		if err != nil {
			log.Printf("%s\n", errors.Wrap(err, "legacyKb.Export"))
			return
		}

		keyName := key.GetName()
		keyType := key.GetType()

		log.Printf("migrating key: '%s (%s)' ...\n", keyName, keyType)

		if keyType != keyring.TypeLocal {
			if err := migrator.Import(keyName, legKeyInfo); err != nil {
				log.Printf("%s\n", errors.Wrap(err, "migrator.Import"))
				return
			}
			continue
		}

		if err := migrator.Import(keyName, legKeyInfo); err != nil {
			log.Printf("%s\n", errors.Wrap(err, "migrator.Import"))
			return
		}
	}

	log.Println("finished migration")
}
