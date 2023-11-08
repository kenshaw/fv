// Command fv is a command-line font viewer tool.
package main

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"image/color"
	_ "image/png"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"github.com/mattn/go-sixel"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/tdewolff/canvas"
	"github.com/tdewolff/canvas/font"
	"github.com/tdewolff/canvas/renderers/rasterizer"
	"golang.org/x/exp/maps"
	"golang.org/x/exp/slices"
	"gopkg.in/go-playground/colors.v1"
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

func run(ctx context.Context, appName, appVersion string, cliargs []string) error {
	var all, list, match bool
	var fg colors.Color = colors.FromStdColor(color.RGBA{R: 0, G: 0, B: 0})
	var bg colors.Color = colors.FromStdColor(color.RGBA{R: 255, G: 255, B: 255})
	var size, margin, dpi int
	style, variant := canvas.FontRegular, canvas.FontNormal
	c := &cobra.Command{
		Use:     appName + " [flags] <font1> [font2, ..., fontN]",
		Short:   appName + ", a font viewer tool",
		Version: appVersion,
		Args: func(_ *cobra.Command, args []string) error {
			switch hasArgs := len(args) != 0; {
			case all && hasArgs,
				list && hasArgs,
				match && !hasArgs:
				return errors.New("requires --all or one or more args, or --list, or --match and one or more args")
			case all && list,
				all && match,
				match && list:
				return errors.New("--all, --list, and --match must be exclusive")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			sysfonts, err := font.FindSystemFonts(font.DefaultFontDirs())
			if err != nil {
				return err
			}
			fgColor, bgColor := convColor(fg), convColor(bg)
			f := do
			switch {
			case all:
				f = doAll
			case list:
				f = doList
			case match:
				f = doMatch
			}
			return f(os.Stdout, sysfonts, Params{
				FG:      fgColor,
				BG:      bgColor,
				Size:    size,
				DPI:     dpi,
				Margin:  margin,
				Style:   style,
				Variant: variant,
				Args:    args,
			})
		},
	}
	c.Flags().BoolVar(&all, "all", false, "show all system fonts")
	c.Flags().BoolVar(&list, "list", false, "list system fonts")
	c.Flags().BoolVar(&match, "match", false, "match system fonts")
	c.Flags().Var(NewColor(&fg), "fg", "foreground color")
	c.Flags().Var(NewColor(&bg), "bg", "background color")
	c.Flags().IntVar(&size, "size", 72, "font size")
	c.Flags().IntVar(&margin, "margin", 2, "margin")
	c.Flags().IntVar(&dpi, "dpi", 100, "dpi")
	c.Flags().Var(NewStyle(&style), "style", "font style")
	c.Flags().Var(NewVariant(&variant), "variant", "font variant")
	c.SetVersionTemplate("{{ .Name }} {{ .Version }}\n")
	c.InitDefaultHelpCmd()
	c.SetArgs(cliargs[1:])
	c.SilenceErrors, c.SilenceUsage = true, false
	return c.ExecuteContext(ctx)
}

type Params struct {
	FG      color.Color
	BG      color.Color
	Size    int
	DPI     int
	Margin  int
	Style   canvas.FontStyle
	Variant canvas.FontVariant
	Args    []string
}

// do renders the specified font queries to w.
func do(w io.Writer, sysfonts *font.SystemFonts, v Params) error {
	var fonts []*Font
	// collect fonts
	for i := 0; i < len(v.Args); i++ {
		f, err := Open(sysfonts, v.Args[i], v.Style)
		if err != nil {
			fmt.Fprintf(w, "error: unable to open arg %d: %v\n", i, err)
		}
		fonts = append(fonts, f...)
	}
	return render(w, fonts, v)
}

// doAll renders all system fonts to w.
func doAll(w io.Writer, sysfonts *font.SystemFonts, v Params) error {
	families := maps.Keys(sysfonts.Fonts)
	slices.Sort(families)
	// collect fonts
	var fonts []*Font
	for _, family := range families {
		styles := maps.Keys(sysfonts.Fonts[family])
		slices.Sort(styles)
		for _, style := range styles {
			fonts = append(fonts, NewFont(sysfonts.Fonts[family][style]))
		}
	}
	return render(w, fonts, v)
}

func doList(w io.Writer, sysfonts *font.SystemFonts, _ Params) error {
	families := maps.Keys(sysfonts.Fonts)
	slices.Sort(families)
	for i := 0; i < len(families); i++ {
		fmt.Fprintln(w, "---")
		fmt.Fprintf(w, "family: %q\n", families[i])
		fmt.Fprintln(w, "styles:")
		styles := maps.Keys(sysfonts.Fonts[families[i]])
		slices.Sort(styles)
		for _, style := range styles {
			fmt.Fprintf(w, "  %s: %s\n", style, sysfonts.Fonts[families[i]][style].Filename)
		}
	}
	return nil
}

func doMatch(w io.Writer, sysfonts *font.SystemFonts, v Params) error {
	for _, name := range v.Args {
		if font := Match(sysfonts, name, v.Style); font != nil {
			font.WriteYAML(w)
		}
	}
	return nil
}

func render(w io.Writer, fonts []*Font, v Params) error {
	for i := 0; i < len(fonts); i++ {
		err := fonts[i].Render(w, v)
		if err != nil {
			fmt.Fprintf(os.Stdout, "%s -- error: %v\n", fonts[i], err)
		}
		nl := []byte{'\n'}
		if i != len(v.Args)-1 && err == nil {
			nl = append(nl, '\n')
		}
		w.Write(nl)
	}
	return nil
}

type Font struct {
	Path   string
	Family string
	Face   string
}

func NewFont(md font.FontMetadata) *Font {
	family := md.Family
	if family == "" {
		family = titleCase(strings.TrimSuffix(filepath.Base(md.Filename), filepath.Ext(md.Filename)))
	}
	return &Font{
		Path:   md.Filename,
		Family: family,
		Face:   fmt.Sprintf("%s (%s)", family, md.Style),
	}
}

func NewFontForPath(path string) *Font {
	return &Font{
		Path:   path,
		Family: titleCase(strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))),
	}
}

func Match(sysfonts *font.SystemFonts, name string, style canvas.FontStyle) *Font {
	md, ok := sysfonts.Match(name, font.ParseStyle(style.String()))
	if !ok {
		return nil
	}
	return NewFont(md)
}

// Open opens fonts.
func Open(sysfonts *font.SystemFonts, name string, style canvas.FontStyle) ([]*Font, error) {
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

var extRE = regexp.MustCompile(`\.(ttf|ttc|otf|woff|woff2|sfnt)$`)

func (font *Font) String() string {
	name := font.Face
	if name == "" {
		name = font.Family
	}
	return fmt.Sprintf("%q: %s", name, font.Path)
}

func (font *Font) WriteYAML(w io.Writer) {
	fmt.Fprintln(w, "---")
	fmt.Fprintf(w, "path: %s\n", font.Path)
	fmt.Fprintf(w, "family: %q\n", font.Family)
	fmt.Fprintf(w, "face: %q\n", font.Face)
}

func (font *Font) Load(style canvas.FontStyle) (*canvas.FontFamily, error) {
	ff := canvas.NewFontFamily(font.Family)
	if err := ff.LoadFontFile(font.Path, style); err != nil {
		return nil, err
	}
	return ff, nil
}

func (font *Font) Render(w io.Writer, v Params) error {
	ff, err := font.Load(v.Style)
	if err != nil {
		return err
	}

	fmt.Fprintf(w, "%s\n", font)
	// create canvas and context
	c := canvas.New(100, 100)
	ctx := canvas.NewContext(c)

	// draw text
	face := ff.Face(float64(v.Size), v.FG, v.Style, v.Variant)
	txt, _, err := face.ToPath("the quick brown fox jumps over the lazy dog")
	if err != nil {
		return err
	}
	ctx.SetZIndex(1)
	ctx.SetFillColor(v.FG)
	ctx.DrawPath(0, 0, txt)

	// fit canvas to context
	c.Fit(float64(v.Margin))

	// draw background
	width, height := ctx.Size()
	ctx.SetZIndex(-1)
	ctx.SetFillColor(v.BG)
	ctx.DrawPath(0, 0, canvas.Rectangle(width, height))

	ctx.Close()

	// rasterize canvas to image
	img := rasterizer.Draw(c, canvas.DPI(float64(v.DPI)), canvas.DefaultColorSpace)
	return sixel.NewEncoder(w).Encode(img)
}

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

type Color struct {
	c *colors.Color
}

func NewColor(c *colors.Color) pflag.Value {
	return Color{
		c: c,
	}
}

func (c Color) String() string {
	return (*c.c).String()
}

func (c Color) Set(s string) error {
	var err error
	*c.c, err = colors.Parse(s)
	return err
}

func (c Color) Type() string {
	return "color"
}

type Style struct {
	v *canvas.FontStyle
}

func NewStyle(v *canvas.FontStyle) pflag.Value {
	return Style{
		v: v,
	}
}

func (v Style) String() string {
	return strings.ToLower(v.v.String())
}

func (v Style) Set(s string) error {
	italic, str := false, strings.ToLower(s)
	if italicRE.MatchString(str) {
		italic, str = true, italicRE.ReplaceAllString(str, "")
	}
	switch strings.TrimSpace(str) {
	case "regular", "", "400", "0":
		*v.v = canvas.FontRegular
	case "thin", "100":
		*v.v = canvas.FontThin
	case "extra-light", "extralight", "200":
		*v.v = canvas.FontExtraLight
	case "light", "300":
		*v.v = canvas.FontLight
	case "medium", "500":
		*v.v = canvas.FontMedium
	case "semi-bold", "semibold", "600":
		*v.v = canvas.FontSemiBold
	case "bold", "700":
		*v.v = canvas.FontBold
	case "extra-bold", "extrabold", "800":
		*v.v = canvas.FontExtraBold
	case "black", "900":
		*v.v = canvas.FontBlack
	default:
		return fmt.Errorf("invalid font style %q", s)
	}
	if italic {
		*v.v |= canvas.FontItalic
	}
	return nil
}

var italicRE = regexp.MustCompile(`(?i)\s*italic\s*`)

func (v Style) Type() string {
	return "font-style"
}

type Variant struct {
	v *canvas.FontVariant
}

func NewVariant(v *canvas.FontVariant) pflag.Value {
	return Variant{
		v: v,
	}
}

func (v Variant) String() string {
	return strings.ToLower(v.v.String())
}

func (v Variant) Set(s string) error {
	switch strings.ToLower(s) {
	case "normal":
		*v.v = canvas.FontNormal
	case "subscript":
		*v.v = canvas.FontSubscript
	case "superscript":
		*v.v = canvas.FontSuperscript
	case "smallcaps":
		*v.v = canvas.FontSmallcaps
	default:
		return fmt.Errorf("invalid font variant %q", s)
	}
	return nil
}

func (v Variant) Type() string {
	return "font-variant"
}

func convColor(c colors.Color) color.Color {
	clr := c.ToRGB()
	return color.RGBA{R: clr.R, G: clr.G, B: clr.B, A: 0xff}
}

var phrases = map[string]string{
	"quick":    "The quick brown fox jumps over the lazy dog",
	"liquor":   "Pack my box with five dozen liquor jugs",
	"jackdaws": "Jackdaws love my big sphinx of quartz",
	"wizards":  "The five boxing wizards jump quickly",
}

//go:embed text.tpl
var textTpl []byte
