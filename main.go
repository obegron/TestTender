package main

import (
	_ "embed"

	"github.com/obegron/testtender/cmd"
)

//go:embed README.md
var readme string

//go:embed config.md
var config string

//go:embed LICENSE
var license string

func main() {
	cmd.README = readme
	cmd.LICENSE = license
	cmd.CONFIG = config
	cmd.Execute()
}
