package main

func restoreFiles(config Config) {
	cm := scanRemoteChunksMap(config)
	restoreChunk(config, cm)
}
