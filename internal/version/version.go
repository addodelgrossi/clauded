// Package version expõe metadados de build injetados via -ldflags.
package version

import "runtime"

// Variáveis preenchidas em tempo de build com:
//
//	-ldflags "-X github.com/addodelgrossi/clauded/internal/version.Version=v1.2.3 ..."
var (
	// Version é a versão semântica do binário (ex.: v1.2.3 ou "dev").
	Version = "dev"
	// Commit é o hash curto do commit de build.
	Commit = "none"
	// Date é a data de build em RFC3339.
	Date = "unknown"
)

// Info agrega os metadados de build para serialização.
type Info struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	Date      string `json:"date"`
	GoVersion string `json:"go_version"`
	OS        string `json:"os"`
	Arch      string `json:"arch"`
}

// Get retorna as informações de versão atuais.
func Get() Info {
	return Info{
		Version:   Version,
		Commit:    Commit,
		Date:      Date,
		GoVersion: runtime.Version(),
		OS:        runtime.GOOS,
		Arch:      runtime.GOARCH,
	}
}
