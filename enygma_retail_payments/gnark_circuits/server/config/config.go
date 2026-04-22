package config

type Config struct {
	Port             string
	PaymentPk        string
	PaymentVk        string
	PrivateMintPk    string
	PrivateMintVk    string
}

func Load() *Config {
	return &Config{
		Port:          "8082",
		PaymentPk:     "./scripts/keys/PaymentPK.key",
		PaymentVk:     "./scripts/keys/PaymentVK.key",
		PrivateMintPk: "./scripts/keys/PrivateMintPK.key",
		PrivateMintVk: "./scripts/keys/PrivateMintVK.key",
	}
}
