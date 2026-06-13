// Package web embute os assets estáticos da UI de demonstração do clauded.
//
// É uma única página HTML (sem build, sem dependências) servida pelo próprio
// daemon em GET /ui — mesma origem da API, logo sem necessidade de CORS.
package web

import _ "embed"

// IndexHTML é a página de chat de demonstração.
//
//go:embed index.html
var IndexHTML []byte
