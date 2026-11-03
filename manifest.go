package project

import (
	_ "embed"
	"encoding/json"
)

//go:embed versions.json
var manifest []byte

type Metadata struct {
	Release string `json:"release"`
	Go      string `json:"go"`
	SingBox struct {
		Minimum string `json:"minimum_version"`
		Tested  string `json:"tested_version"`
		AMD64   string `json:"linux_amd64_sha256"`
		ARM64   string `json:"linux_arm64_sha256"`
	} `json:"sing_box"`
}

var Versions = func() Metadata {
	var v Metadata
	if err := json.Unmarshal(manifest, &v); err != nil {
		panic(err)
	}
	return v
}()
