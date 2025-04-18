// Command fv is a command-line font viewer using terminal graphics (Sixel,
// iTerm, Kitty).
package main

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"regexp"
	"slices"
	"strings"
	"text/template"

	"github.com/kenshaw/colors"
	"github.com/kenshaw/fontimg"
	"github.com/kenshaw/rasterm"
	"github.com/spf13/pflag"
	"github.com/tdewolff/canvas"
	fontpkg "github.com/tdewolff/font"
	"github.com/xo/ox"
	_ "github.com/xo/ox/color"
)

var (
	name    = "fv"
	version = "0.0.0-dev"
)

func main() {
	ox.DefaultVersionString = version
	args := &Args{}
	ox.RunContext(
		context.Background(),
		ox.Exec(args.run(os.Stdout)),
		ox.Usage(name, "a command-line font viewer using terminal graphics"),
		ox.Defaults(),
		ox.From(args),
	)
}

func (args *Args) run(w io.Writer) func(context.Context, []string) error {
	return func(ctx context.Context, cliargs []string) error {
		// style, variant := canvas.FontRegular, canvas.FontNormal
		sysfonts, err := fontpkg.FindSystemFonts(fontpkg.DefaultFontDirs())
		if err != nil {
			return err
		}
		switch hasArgs := len(cliargs) != 0; {
		case args.All && hasArgs:
			return errors.New("--all does not take any args")
		case args.List && hasArgs:
			return errors.New("--list does not take any args")
		case args.Match && !hasArgs:
			return errors.New("--match requires one or more args")
		case args.All && args.List,
			args.All && args.Match,
			args.Match && args.List:
			return errors.New("--all, --list, and --match are exclusive")
		}
		f := args.do
		switch {
		case args.All:
			f = args.doAll
		case args.List:
			f = args.doList
		case args.Match:
			f = args.doMatch
		}
		return f(w, sysfonts, cliargs)
	}
}

type Args struct {
	All     bool               `ox:"show all system fonts"`
	List    bool               `ox:"list system fonts"`
	Match   bool               `ox:"match system fonts"`
	Fg      *colors.Color      `ox:"foreground color,default:black"`
	Bg      *colors.Color      `ox:"background color,default:white"`
	Size    uint               `ox:"font size,default:48"`
	Dpi     uint               `ox:"dpi,default:100"`
	Margin  uint               `ox:"margin,default:5"`
	Style   canvas.FontStyle   `ox:"font style"`
	Variant canvas.FontVariant `ox:"font variant"`
	Text    string             `ox:"display text"`
}

// do renders the specified font queries to w.
func (args *Args) do(w io.Writer, sysfonts *fontpkg.SystemFonts, cliargs []string) error {
	if !rasterm.Available() {
		return rasterm.ErrTermGraphicsNotAvailable
	}
	var fonts []*fontimg.Font
	// collect fonts
	for i := range cliargs {
		v, err := fontimg.Open(cliargs[i], args.Style, sysfonts)
		if err != nil {
			fmt.Fprintf(w, "error: unable to open arg %d: %v\n", i, err)
		}
		fonts = append(fonts, v...)
	}
	return args.render(w, fonts)
}

// doAll renders all system fonts to w.
func (args *Args) doAll(w io.Writer, sysfonts *fontpkg.SystemFonts, cliargs []string) error {
	if !rasterm.Available() {
		return rasterm.ErrTermGraphicsNotAvailable
	}
	// collect fonts
	var fonts []*fontimg.Font
	for _, family := range slices.Sorted(maps.Keys(sysfonts.Fonts)) {
		for _, style := range slices.Sorted(maps.Keys(sysfonts.Fonts[family])) {
			fonts = append(fonts, fontimg.NewFont(sysfonts.Fonts[family][style]))
		}
	}
	return args.render(w, fonts)
}

func (args *Args) doList(w io.Writer, sysfonts *fontpkg.SystemFonts, _ []string) error {
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

func (args *Args) doMatch(w io.Writer, sysfonts *fontpkg.SystemFonts, cliargs []string) error {
	for _, name := range cliargs {
		if font := fontimg.Match(name, args.Style, sysfonts); font != nil {
			font.WriteYAML(w)
		}
	}
	return nil
}

func (args *Args) render(w io.Writer, fonts []*fontimg.Font) error {
	var tpl *template.Template
	if args.Text != "" {
		var err error
		if tpl, err = fontimg.NewTemplate(args.Text); err != nil {
			return err
		}
	}
	for i := range fonts {
		fmt.Fprintf(w, "%s:\n", fonts[i].Family)
		img, err := fonts[i].Rasterize(
			tpl,
			int(args.Size),
			args.Style,
			args.Variant,
			args.Fg,
			args.Bg,
			float64(args.Dpi),
			float64(args.Margin),
		)
		if err != nil {
			fmt.Fprintf(os.Stdout, "%s -- error: %v\n", fonts[i], err)
			continue
		}
		if err := rasterm.Encode(w, img); err != nil {
			fmt.Fprintf(os.Stdout, "%s -- error: %v\n", fonts[i], err)
			continue
		}
		if i != len(fonts)-1 {
			fmt.Fprintln(w)
		}
	}
	return nil
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
