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
	var fg colors.Color = colors.FromStdColor(color.RGBA{R: 0, G: 0, B: 0})
	var bg colors.Color = colors.FromStdColor(color.RGBA{R: 255, G: 255, B: 255})
	var margin int
	var dpi int
	var all bool
	c := &cobra.Command{
		Use:     appName + " [flags] <font1> [font2, ..., fontN]",
		Short:   appName + ", a font viewer tool",
		Version: appVersion,
		Args: func(_ *cobra.Command, args []string) error {
			if !all && len(args) < 1 {
				return errors.New("requires --all or at least 1 args")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			sysfonts, err := font.FindSystemFonts(font.DefaultFontDirs())
			if err != nil {
				return err
			}
			fgColor, bgColor := convColor(fg), convColor(bg)
			if all {
				return renderAll(os.Stdout, sysfonts, fgColor, bgColor, dpi, margin)
			}
			if err := render(os.Stdout, sysfonts, fgColor, bgColor, dpi, margin, args); err != nil {
				return err
			}
			return nil
		},
	}
	c.Flags().Var(NewColor(&fg), "fg", "foreground color")
	c.Flags().Var(NewColor(&bg), "bg", "background color")
	c.Flags().IntVar(&margin, "margin", 2, "margin")
	c.Flags().IntVar(&dpi, "dpi", 100, "dpi")
	c.Flags().BoolVar(&all, "all", false, "show all system fonts")
	c.SetVersionTemplate("{{ .Name }} {{ .Version }}\n")
	c.InitDefaultHelpCmd()
	c.SetArgs(cliargs[1:])
	c.SilenceErrors, c.SilenceUsage = true, false
	return c.ExecuteContext(ctx)
}

func convColor(c colors.Color) color.Color {
	clr := c.ToRGB()
	return color.RGBA{R: clr.R, G: clr.G, B: clr.B, A: 0xff}
}

func render(w io.Writer, sysfonts *font.SystemFonts, fg, bg color.Color, dpi, margin int, v []string) error {
	for i := 0; i < len(v); i++ {
		name, pathstr, ff, err := openFont(sysfonts, v[i])
		if err != nil {
			if name == "" {
				fmt.Fprintf(w, "error: arg %d: %v\n", i, err)
			} else {
				fmt.Fprintf(w, "%q %s -- error: %v\n", name, pathstr, err)
			}
		} else if err := renderFont(w, fg, bg, dpi, margin, pathstr, name, ff); err != nil {
			return err
		}
		nl := []byte{'\n'}
		if i != len(v)-1 && err == nil {
			nl = append(nl, '\n')
		}
		w.Write(nl)
	}
	return nil
}

func renderAll(w io.Writer, sysfonts *font.SystemFonts, fg, bg color.Color, dpi, margin int) error {
	keys := maps.Keys(sysfonts.Fonts)
	slices.Sort(keys)
	for i := 0; i < len(keys); i++ {
		style := font.Regular
		font, ok := sysfonts.Fonts[keys[i]][style]
		if !ok {
			styles := maps.Keys(sysfonts.Fonts[keys[i]])
			slices.Sort(styles)
			font, style = sysfonts.Fonts[keys[i]][styles[0]], styles[0]
		}
		ff := canvas.NewFontFamily(font.Family)
		if err := ff.LoadFontFile(font.Filename, canvas.FontRegular); err != nil {
			fmt.Fprintf(w, "%q %s -- error: %v\n", font.Family, font.Filename, err)
			if i != len(keys)-1 {
				w.Write([]byte{'\n'})
			}
			continue
		}
		if err := renderFont(w, fg, bg, dpi, margin, font.Filename, font.Family, ff); err != nil {
			return err
		}
		nl := []byte{'\n'}
		if i != len(keys)-1 {
			nl = append(nl, '\n')
		}
		if _, err := w.Write(nl); err != nil {
			return err
		}
	}
	return nil
}

func renderFont(w io.Writer, fg, bg color.Color, dpi, margin int, pathstr, name string, font *canvas.FontFamily) error {
	fmt.Fprintf(w, "%q %s\n", name, pathstr)
	// create canvas and context
	c := canvas.New(100, 100)
	ctx := canvas.NewContext(c)

	// draw text
	face := font.Face(72, fg, canvas.FontRegular, canvas.FontNormal)
	txt, _, err := face.ToPath("the quick brown fox jumps over the lazy dog")
	if err != nil {
		return err
	}
	ctx.SetZIndex(1)
	ctx.SetFillColor(fg)
	ctx.DrawPath(0, 0, txt)

	// fit canvas to context
	c.Fit(float64(margin))

	// draw background
	width, height := ctx.Size()
	ctx.SetZIndex(-1)
	ctx.SetFillColor(bg)
	ctx.DrawPath(0, 0, canvas.Rectangle(width, height))

	ctx.Close()

	// rasterize canvas to image
	img := rasterizer.Draw(c, canvas.DPI(float64(dpi)), canvas.DefaultColorSpace)
	return sixel.NewEncoder(w).Encode(img)
}

// openFont opens the specified font.
func openFont(sysfonts *font.SystemFonts, query string) (string, string, *canvas.FontFamily, error) {
	var family string
	var pathstr string
	if fileExists(query) {
		family = titleCase(strings.TrimSuffix(filepath.Base(query), filepath.Ext(query)))
		pathstr = query
	} else {
		f, ok := sysfonts.Match(query, font.Regular)
		if !ok {
			return "", "", nil, fmt.Errorf("unable to match font %q", query)
		}
		family = fontName(f)
		pathstr = f.Filename
	}
	font := canvas.NewFontFamily(family)
	if err := font.LoadFontFile(pathstr, canvas.FontRegular); err != nil {
		return family, pathstr, nil, err
	}
	return family, pathstr, font, nil
}

func fontName(f font.FontMetadata) string {
	switch {
	case f.Family != "":
		return f.Family
	}
	return titleCase(strings.TrimSuffix(filepath.Base(f.Filename), filepath.Ext(f.Filename)))
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

func peek(r []rune, i int) rune {
	if i < len(r) {
		return r[i]
	}
	return 0
}

var spaceRE = regexp.MustCompile(`\s+`)

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

func fileExists(name string) bool {
	fi, err := os.Stat(name)
	return err == nil && !fi.IsDir()
}

var phrases = map[string]string{
	"quick":    "The quick brown fox jumps over the lazy dog",
	"liquor":   "Pack my box with five dozen liquor jugs",
	"jackdaws": "Jackdaws love my big sphinx of quartz",
	"wizards":  "The five boxing wizards jump quickly",
}

//go:embed text.tpl
var textTpl []byte
