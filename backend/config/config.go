package config

import "os"

type Config struct {
	DataDog_Service    string
	DataDog_ENV        string
	DataDog_VERSION    string
	DataDog_API_KEY    string
	DataDog_SITE       string
	DataDog_AGENT_HOST string
}

func Load() Config {
	return Config{
		DataDog_Service:    os.Getenv("DD_SERVICE"),
		DataDog_ENV:        os.Getenv("DD_ENV"),
		DataDog_VERSION:    os.Getenv("DD_VERSION"),
		DataDog_API_KEY:    os.Getenv("DD_API_KEY"),
		DataDog_SITE:       os.Getenv("DD_SITE"),
		DataDog_AGENT_HOST: os.Getenv("DD_AGENT_HOST"),
	}
}
