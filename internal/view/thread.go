package view

import (
	_ "embed"
	"io"
	"text/template"
)

//go:embed thread.tmpl
var threadTemplate string

type ThreadData struct {
	Pod       string
	Namespace string
	Owner     string
	Status    string
	Reason    string
}

func Thread(data ThreadData, writer io.Writer) error {
	tmpl, err := template.New("thread").Parse(threadTemplate)
	if err != nil {
		return err
	}
	return tmpl.Execute(writer, data)
}
