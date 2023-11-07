{{ size (inc .Size 4) }}{{ .Name }}, {{ .Weight }}
{{ size .Size }}abcdefghijklmnopqrstuvwxyz
ABCDEFGHIJKLMNOPQRSTUVWXYZ
0123456789.<:,>;('~"){!@#$%^&*?`=}[_\-/+]
{{ range $i := (step .Size (inc .Size ) 2) }}{{ size $i }}{{ range $phrase := .Phrases }}{{ phrase }}.
{{ end }}{{ end }}
