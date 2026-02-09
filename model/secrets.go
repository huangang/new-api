package model

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"strings"

	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	optionKeySessionSecret = "SessionSecret"
	optionKeyCryptoSecret  = "CryptoSecret"
)

func EnsurePersistentSecrets() error {
	sessionSecretFromEnv := strings.TrimSpace(os.Getenv("SESSION_SECRET"))
	cryptoSecretFromEnv := strings.TrimSpace(os.Getenv("CRYPTO_SECRET"))

	sessionSecret, sessionSecretSource, err := resolveSessionSecret(sessionSecretFromEnv)
	if err != nil {
		return err
	}

	generatedSessionSecret := sessionSecretSource == "generated"

	// Persist secrets into DB (for restart durability) when env not set.
	if generatedSessionSecret {
		if err := storeSessionSecretIfAbsent(sessionSecret); err != nil {
			return err
		}
		storedValue, found, err := getOptionValue(optionKeySessionSecret)
		if err != nil {
			return err
		}
		storedValue = strings.TrimSpace(storedValue)
		if found && storedValue != "" {
			sessionSecret = storedValue
			sessionSecretSource = "db"
		}
	}
	common.SessionSecret = sessionSecret

	cryptoSecret, err := resolveCryptoSecret(cryptoSecretFromEnv, sessionSecret)
	if err != nil {
		return err
	}
	common.CryptoSecret = cryptoSecret

	// If secrets are explicitly configured via env, we still store them so a later
	// restart without env won't invalidate existing sessions.
	if sessionSecretSource == "env" {
		if err := upsertOption(optionKeySessionSecret, sessionSecret); err != nil {
			return err
		}
	}
	if cryptoSecretFromEnv != "" {
		if err := upsertOption(optionKeyCryptoSecret, cryptoSecretFromEnv); err != nil {
			return err
		}
	}

	switch sessionSecretSource {
	case "env":
		common.SysLog("SESSION_SECRET loaded from environment")
	case "db":
		if generatedSessionSecret {
			common.SysLog("SESSION_SECRET was not set; generated and stored in database options")
		} else {
			common.SysLog("SESSION_SECRET loaded from database options")
		}
	case "generated":
		common.SysLog("SESSION_SECRET was not set; generated and stored in database options")
	}
	return nil
}

func resolveSessionSecret(envValue string) (value string, source string, err error) {
	if envValue != "" {
		return envValue, "env", nil
	}
	optValue, found, err := getOptionValue(optionKeySessionSecret)
	if err != nil {
		return "", "", err
	}
	optValue = strings.TrimSpace(optValue)
	if found && optValue != "" {
		return optValue, "db", nil
	}

	generated, err := generateSecretHex(32)
	if err != nil {
		return "", "", err
	}
	return generated, "generated", nil
}

func resolveCryptoSecret(envValue string, sessionSecret string) (string, error) {
	if envValue != "" {
		return envValue, nil
	}

	optValue, found, err := getOptionValue(optionKeyCryptoSecret)
	if err != nil {
		return "", err
	}
	optValue = strings.TrimSpace(optValue)
	if found && optValue != "" {
		return optValue, nil
	}

	// Keep legacy behavior: default CRYPTO_SECRET to SESSION_SECRET.
	return sessionSecret, nil
}

func getOptionValue(key string) (string, bool, error) {
	var opt Option
	err := DB.Where(&Option{Key: key}).Take(&opt).Error
	if err == nil {
		return opt.Value, true, nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", false, nil
	}
	return "", false, err
}

func upsertOption(key string, value string) error {
	return DB.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "key"}},
		DoUpdates: clause.AssignmentColumns([]string{"value"}),
	}).Create(&Option{Key: key, Value: value}).Error
}

func storeSessionSecretIfAbsent(secret string) error {
	return DB.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "key"}},
		DoNothing: true,
	}).Create(&Option{Key: optionKeySessionSecret, Value: secret}).Error
}

func generateSecretHex(byteLen int) (string, error) {
	if byteLen <= 0 {
		return "", nil
	}
	buf := make([]byte, byteLen)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}
