package config


type Config struct {
    Port     string
   
    PrivateMintPk string
    PrivateMintVk string

    DvPInitiatorPk   string
    DvPInitiatorVk   string
    DvPDestinationPk string
    DvPDestinationVk string
}

func Load() *Config {
    return &Config{
        Port:    	   "8081",
     
        PrivateMintPk: "./scripts/keys/PrivateMintPK.key",
        PrivateMintVk: "./scripts/keys/PrivateMintVK.key",
        DvPInitiatorPk:   "./scripts/keys/DvPInitiatorPK.key",
        DvPInitiatorVk:   "./scripts/keys/DvPInitiatorVK.key",
        DvPDestinationPk: "./scripts/keys/DvPDestinationPK.key",
        DvPDestinationVk: "./scripts/keys/DvPDestinationVK.key",
    }
}

