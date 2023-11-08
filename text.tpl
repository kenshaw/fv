{{ size (inc .Size 2) }}{{ .Name }}, {{ .Style }}
{{ size .Size }}{{ if .SampleText }}
{{ .SampleText }}{{ else }}abcdefghijklmnopqrstuvwxyz
ABCDEFGHIJKLMNOPQRSTUVWXYZ
0123456789.<:,>;('~"){!@#$%^&*?`=}[_\-/+]
The quick brown fox jumps over the lazy dog.
{{ size (inc .Size 6) }}Pack my box with five dozen liquor jugs.
{{ size (inc .Size 12) }}Jackdaws love my big sphinx of quartz.
{{ size (inc .Size 18) }}The five boxing wizards jump quickly.{{ end }}
