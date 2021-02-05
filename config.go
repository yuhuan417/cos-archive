package main

import (
	"encoding/json"
	"io/ioutil"
	"os"
)

type Config struct {
	FilePaths []string
	SkipList  []string
	Threads   int
	Retry   int
	WorkingDir string
	COS       COSConfig
}

type COSConfig struct {
	URL    string
	ID     string
	Key    string
	Prefix string
}

func getConfig(configFileName string) (config Config) {
	// default value
	config.Threads = 2

	jsonFile, err := os.Open(configFileName)

	if err != nil {
		return
	}

	defer jsonFile.Close()

	byteValue, _ := ioutil.ReadAll(jsonFile)

	json.Unmarshal(byteValue, &config)

	return
}
