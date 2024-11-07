// Command fv is a command-line font viewer using terminal graphics (Sixel,
// iTerm, Kitty).
package main

import (
	"bytes"
	"context"
	_ "embed"
	"errors"
	"fmt"
	"image/color"
	"io"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"text/template"
	"unicode"

	"github.com/kenshaw/colors"
	"github.com/kenshaw/rasterm"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/tdewolff/canvas"
	"github.com/tdewolff/canvas/renderers/rasterizer"
	fontpkg "github.com/tdewolff/font"
)

var (
	name    = "fv"
	version = "0.0.0-dev"
)

func main() {
	if err := run(context.Background(), name, version, os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, name, version string, cliargs []string) error {
	var all, list, match bool
	fg, bg := colors.FromColor(color.Black), colors.FromColor(color.White)
	var size, margin, dpi int
	style, variant := canvas.FontRegular, canvas.FontNormal
	var text string
	var (
		bashCompletion       bool
		zshCompletion        bool
		fishCompletion       bool
		powershellCompletion bool
		noDescriptions       bool
	)
	c := &cobra.Command{
		Use:           name + " [flags] <font1> [font2, ..., fontN]",
		Short:         name + ", a command-line font viewer using terminal graphics",
		Version:       version,
		SilenceErrors: true,
		SilenceUsage:  false,
		Args: func(_ *cobra.Command, args []string) error {
			switch hasArgs := len(args) != 0; {
			case all && hasArgs:
				return errors.New("--all does not take any args")
			case list && hasArgs:
				return errors.New("--list does not take any args")
			case match && !hasArgs:
				return errors.New("--match requires one or more args")
			case all && list,
				all && match,
				match && list:
				return errors.New("--all, --list, and --match are exclusive")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			// completions and short circuits
			switch {
			case bashCompletion:
				return cmd.GenBashCompletionV2(os.Stdout, !noDescriptions)
			case zshCompletion:
				if noDescriptions {
					return cmd.GenZshCompletionNoDesc(os.Stdout)
				}
				return cmd.GenZshCompletion(os.Stdout)
			case fishCompletion:
				return cmd.GenFishCompletion(os.Stdout, !noDescriptions)
			case powershellCompletion:
				if noDescriptions {
					return cmd.GenPowerShellCompletion(os.Stdout)
				}
				return cmd.GenPowerShellCompletionWithDesc(os.Stdout)
			}
			sysfonts, err := fontpkg.FindSystemFonts(fontpkg.DefaultFontDirs())
			if err != nil {
				return err
			}
			f := do
			switch {
			case all:
				f = doAll
			case list:
				f = doList
			case match:
				f = doMatch
			}
			return f(os.Stdout, sysfonts, &Args{
				FG:      fg,
				BG:      bg,
				Size:    size,
				DPI:     dpi,
				Margin:  margin,
				Style:   style,
				Variant: variant,
				Text:    text,
				Args:    args,
			})
		},
	}
	c.SetVersionTemplate("{{ .Name }} {{ .Version }}\n")
	c.InitDefaultHelpCmd()
	c.SetArgs(cliargs[1:])
	flags := c.Flags()
	flags.BoolVar(&all, "all", false, "show all system fonts")
	flags.BoolVar(&list, "list", false, "list system fonts")
	flags.BoolVar(&match, "match", false, "match system fonts")
	flags.Var(fg.Pflag(), "fg", "foreground color")
	flags.Var(bg.Pflag(), "bg", "background color")
	flags.IntVar(&size, "size", 48, "font size")
	flags.IntVar(&margin, "margin", 5, "margin")
	flags.IntVar(&dpi, "dpi", 100, "dpi")
	flags.Var(NewStyle(&style), "style", "font style")
	flags.Var(NewVariant(&variant), "variant", "font variant")
	flags.StringVar(&text, "text", "", "display text")
	// completions
	flags.BoolVar(&bashCompletion, "completion-script-bash", false, "output bash completion script and exit")
	flags.BoolVar(&zshCompletion, "completion-script-zsh", false, "output zsh completion script and exit")
	flags.BoolVar(&fishCompletion, "completion-script-fish", false, "output fish completion script and exit")
	flags.BoolVar(&powershellCompletion, "completion-script-powershell", false, "output powershell completion script and exit")
	flags.BoolVar(&noDescriptions, "no-descriptions", false, "disable descriptions in completion scripts")
	// mark hidden
	for _, name := range []string{
		"completion-script-bash", "completion-script-zsh", "completion-script-fish",
		"completion-script-powershell", "no-descriptions",
	} {
		flags.Lookup(name).Hidden = true
	}
	return c.ExecuteContext(ctx)
}

type Args struct {
	FG      color.Color
	BG      color.Color
	Size    int
	DPI     int
	Margin  int
	Style   canvas.FontStyle
	Variant canvas.FontVariant
	Text    string
	Args    []string
	once    sync.Once
	tpl     *template.Template
}

func (args *Args) Template() (*template.Template, error) {
	var err error
	args.once.Do(func() {
		s := args.Text
		if s == "" {
			s = string(textTpl)
		}
		args.tpl, err = template.New("").Funcs(map[string]interface{}{
			"size": func(size int) string {
				return fmt.Sprintf("\x00%d\x00", size)
			},
			"inc": func(a, b int) int {
				return a + b
			},
		}).Parse(s)
	})
	switch {
	case err != nil:
		return nil, err
	case args.tpl == nil:
		return nil, errors.New("invalid template state")
	}
	return args.tpl, nil
}

// do renders the specified font queries to w.
func do(w io.Writer, sysfonts *fontpkg.SystemFonts, args *Args) error {
	if !rasterm.Available() {
		return rasterm.ErrTermGraphicsNotAvailable
	}
	var fonts []*Font
	// collect fonts
	for i := 0; i < len(args.Args); i++ {
		v, err := Open(sysfonts, args.Args[i], args.Style)
		if err != nil {
			fmt.Fprintf(w, "error: unable to open arg %d: %v\n", i, err)
		}
		fonts = append(fonts, v...)
	}
	return render(w, fonts, args)
}

// doAll renders all system fonts to w.
func doAll(w io.Writer, sysfonts *fontpkg.SystemFonts, args *Args) error {
	if !rasterm.Available() {
		return rasterm.ErrTermGraphicsNotAvailable
	}
	// collect fonts
	var fonts []*Font
	for _, family := range slices.Sorted(maps.Keys(sysfonts.Fonts)) {
		for _, style := range slices.Sorted(maps.Keys(sysfonts.Fonts[family])) {
			fonts = append(fonts, NewFont(sysfonts.Fonts[family][style]))
		}
	}
	return render(w, fonts, args)
}

func doList(w io.Writer, sysfonts *fontpkg.SystemFonts, _ *Args) error {
	for _, family := range slices.Sorted(maps.Keys(sysfonts.Fonts)) {
		fmt.Fprintln(w, "---")
		fmt.Fprintf(w, "family: %q\n", family)
		fmt.Fprintln(w, "styles:")
		for _, style := range slices.Sorted(maps.Keys(sysfonts.Fonts[family])) {
			fmt.Fprintf(w, "  %s: %s\n", style, sysfonts.Fonts[family][style].Filename)
		}
	}
	return nil
}

func doMatch(w io.Writer, sysfonts *fontpkg.SystemFonts, args *Args) error {
	for _, name := range args.Args {
		if font := Match(sysfonts, name, args.Style); font != nil {
			font.WriteYAML(w)
		}
	}
	return nil
}

func render(w io.Writer, fonts []*Font, args *Args) error {
	tpl, err := args.Template()
	if err != nil {
		return err
	}
	for i := 0; i < len(fonts); i++ {
		if err := fonts[i].Render(w, tpl, args); err != nil {
			fmt.Fprintf(os.Stdout, "%s -- error: %v\n", fonts[i], err)
		}
		if i != len(fonts)-1 {
			fmt.Fprintln(w)
		}
	}
	return nil
}

type TemplateData struct {
	Size       int
	Name       string
	Style      string
	SampleText string
}

type Font struct {
	Path       string
	Family     string
	Name       string
	Style      string
	SampleText string
	once       sync.Once
}

func NewFont(md fontpkg.FontMetadata) *Font {
	family := md.Family
	if family == "" {
		family = titleCase(strings.TrimSuffix(filepath.Base(md.Filename), filepath.Ext(md.Filename)))
	}
	return &Font{
		Path:   md.Filename,
		Family: family,
		Style:  md.Style.String(),
	}
}

func NewFontForPath(path string) *Font {
	return &Font{
		Path:   path,
		Family: titleCase(strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))),
	}
}

func Match(sysfonts *fontpkg.SystemFonts, name string, style canvas.FontStyle) *Font {
	md, ok := sysfonts.Match(name, fontpkg.ParseStyle(style.String()))
	if !ok {
		return nil
	}
	return NewFont(md)
}

// Open opens fonts.
func Open(sysfonts *fontpkg.SystemFonts, name string, style canvas.FontStyle) ([]*Font, error) {
	var v []*Font
	switch fi, err := os.Stat(name); {
	case err == nil && fi.IsDir():
		entries, err := os.ReadDir(name)
		if err != nil {
			return nil, fmt.Errorf("unable to open directory %q: %v", name, err)
		}
		for _, entry := range entries {
			if s := entry.Name(); !entry.IsDir() && extRE.MatchString(s) {
				v = append(v, NewFontForPath(filepath.Join(name, s)))
			}
		}
		sort.Slice(v, func(i, j int) bool {
			return strings.ToLower(v[i].Family) < strings.ToLower(v[j].Family)
		})
	case err == nil:
		v = append(v, NewFontForPath(name))
	default:
		if font := Match(sysfonts, name, style); font != nil {
			v = append(v, font)
		}
	}
	if len(v) == 0 {
		return nil, fmt.Errorf("unable to locate font %q", name)
	}
	return v, nil
}

var extRE = regexp.MustCompile(`(?i)\.(ttf|ttc|otf|woff|woff2|sfnt)$`)

func (font *Font) BestName() string {
	if font.Name != "" {
		return font.Name
	}
	return font.Family
}

func (font *Font) String() string {
	name := font.BestName()
	if font.Style != "" {
		name += " (" + font.Style + ")"
	}
	return fmt.Sprintf("%q: %s", name, font.Path)
}

func (font *Font) WriteYAML(w io.Writer) {
	fmt.Fprintln(w, "---")
	fmt.Fprintf(w, "path: %s\n", font.Path)
	fmt.Fprintf(w, "family: %q\n", font.BestName())
	fmt.Fprintf(w, "style: %q\n", font.Style)
}

func (font *Font) Load(style canvas.FontStyle) (*canvas.FontFamily, error) {
	ff := canvas.NewFontFamily(font.Family)
	if err := ff.LoadFontFile(font.Path, style); err != nil {
		return nil, err
	}
	font.once.Do(func() {
		face := ff.Face(16)
		if v := face.Font.SFNT.Name.Get(fontpkg.NameFontFamily); 0 < len(v) {
			font.Name = v[0].String()
		}
		if v := face.Font.SFNT.Name.Get(fontpkg.NameFontSubfamily); 0 < len(v) {
			font.Style = fontpkg.ParseStyle(v[0].String()).String()
		}
		if v := face.Font.SFNT.Name.Get(fontpkg.NameSampleText); 0 < len(v) {
			font.SampleText = v[0].String()
		}
	})
	return ff, nil
}

func (font *Font) Render(w io.Writer, tpl *template.Template, args *Args) error {
	// load font family
	ff, err := font.Load(args.Style)
	if err != nil {
		return err
	}

	// generate text
	buf := new(bytes.Buffer)
	if err := tpl.Execute(buf, TemplateData{
		Size:       args.Size,
		Name:       font.BestName(),
		Style:      font.Style,
		SampleText: font.SampleText,
	}); err != nil {
		return err
	}

	fmt.Fprintf(w, "%s\n", font)

	// create canvas and context
	c := canvas.New(100, 100)
	ctx := canvas.NewContext(c)

	ctx.SetZIndex(1)
	ctx.SetFillColor(args.FG)

	// draw text
	lines, sizes := breakLines(buf.Bytes(), args.Size)
	for i, y := 0, float64(0); i < len(lines); i++ {
		face := ff.Face(float64(sizes[i]), args.FG, args.Style, args.Variant)
		txt := canvas.NewTextBox(face, strings.TrimSpace(lines[i]), 0, 0, canvas.Left, canvas.Top, 0, 0)
		b := txt.Bounds()
		ctx.DrawText(0, y, txt)
		y += b.Y1 - b.Y0
	}

	// fit canvas to context
	c.Fit(float64(args.Margin))

	// draw background
	ctx.SetZIndex(-1)
	ctx.SetFillColor(args.BG)
	width, height := ctx.Size()
	ctx.DrawPath(0, 0, canvas.Rectangle(width, height))

	ctx.Close()

	// encode
	return rasterm.Encode(w, rasterizer.Draw(
		c,
		canvas.DPI(float64(args.DPI)),
		canvas.DefaultColorSpace,
	))
}

func breakLines(buf []byte, size int) ([]string, []int) {
	var lines []string
	var sizes []int
	for _, line := range bytes.Split(buf, []byte{'\n'}) {
		sz := size
		if m := sizeRE.FindSubmatch(line); m != nil {
			if s, err := strconv.Atoi(string(m[1])); err == nil {
				sz = s
			}
			line = m[2]
		}
		lines, sizes = append(lines, string(line)), append(sizes, sz)
	}
	return lines, sizes
}

var sizeRE = regexp.MustCompile(`^\x00([0-9]+)\x00(.*)$`)

func titleCase(name string) string {
	var prev rune
	var s []rune
	r := []rune(name)
	for i, c := range r {
		switch {
		case unicode.IsLower(prev) && unicode.IsUpper(c):
			s = append(s, ' ')
		case !unicode.IsLetter(c):
			c = ' '
		}
		if unicode.IsUpper(prev) && unicode.IsUpper(c) && unicode.IsLower(peek(r, i+1)) {
			s = append(s, ' ')
		}
		s = append(s, c)
		prev = c
	}
	return spaceRE.ReplaceAllString(strings.TrimSpace(string(s)), " ")
}

var spaceRE = regexp.MustCompile(`\s+`)

func peek(r []rune, i int) rune {
	if i < len(r) {
		return r[i]
	}
	return 0
}

type Style struct {
	fontStyle *canvas.FontStyle
}

func NewStyle(fontStyle *canvas.FontStyle) pflag.Value {
	return Style{
		fontStyle: fontStyle,
	}
}

func (style Style) String() string {
	return strings.ToLower(style.fontStyle.String())
}

func (style Style) Set(s string) error {
	italic, str := false, strings.ToLower(s)
	if italicRE.MatchString(str) {
		italic, str = true, italicRE.ReplaceAllString(str, "")
	}
	switch strings.TrimSpace(str) {
	case "regular", "", "400", "0":
		*style.fontStyle = canvas.FontRegular
	case "thin", "100":
		*style.fontStyle = canvas.FontThin
	case "extra-light", "extralight", "200":
		*style.fontStyle = canvas.FontExtraLight
	case "light", "300":
		*style.fontStyle = canvas.FontLight
	case "medium", "500":
		*style.fontStyle = canvas.FontMedium
	case "semi-bold", "semibold", "600":
		*style.fontStyle = canvas.FontSemiBold
	case "bold", "700":
		*style.fontStyle = canvas.FontBold
	case "extra-bold", "extrabold", "800":
		*style.fontStyle = canvas.FontExtraBold
	case "black", "900":
		*style.fontStyle = canvas.FontBlack
	default:
		return fmt.Errorf("invalid font style %q", s)
	}
	if italic {
		*style.fontStyle |= canvas.FontItalic
	}
	return nil
}

var italicRE = regexp.MustCompile(`(?i)\s*italic\s*`)

func (style Style) Type() string {
	return "font-style"
}

type Variant struct {
	fontVariant *canvas.FontVariant
}

func NewVariant(fontVariant *canvas.FontVariant) pflag.Value {
	return Variant{
		fontVariant: fontVariant,
	}
}

func (variant Variant) String() string {
	return strings.ToLower(variant.fontVariant.String())
}

func (variant Variant) Set(s string) error {
	switch strings.ToLower(s) {
	case "normal":
		*variant.fontVariant = canvas.FontNormal
	case "subscript":
		*variant.fontVariant = canvas.FontSubscript
	case "superscript":
		*variant.fontVariant = canvas.FontSuperscript
	case "smallcaps":
		*variant.fontVariant = canvas.FontSmallcaps
	default:
		return fmt.Errorf("invalid font variant %q", s)
	}
	return nil
}

func (variant Variant) Type() string {
	return "font-variant"
}

//go:embed text.tpl
var textTpl []byte
