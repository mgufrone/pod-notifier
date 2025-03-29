package view

import (
	_ "embed"
	"io"
	v1 "k8s.io/api/core/v1"
	"strings"
	"text/template"
)

//go:embed reply.tmpl
var replyTemplate string

type ReplyData struct {
	Pod    v1.Pod
	Events []v1.Event
}

func Reply(data ReplyData, writer io.Writer) error {
	tmpl, err := template.New("reply").Funcs(map[string]any{
		"contains": strings.Contains,
		"lower":    strings.ToLower,
		"sub": func(a, b int) int {
			return a - b
		},
	}).Parse(replyTemplate)
	if err != nil {
		return err
	}
	return tmpl.Execute(writer, data)
}
