# fv

`fv` is a command-line font viewer using terminal graphics (Sixel, iTerm,
Kitty).

<p align="center">
  <a href="#installing" title="Installing">Installing</a> |
  <a href="#building" title="Building">Building</a> |
  <a href="#using" title="Using">Using</a> |
  <a href="https://github.com/kenshaw/fv/releases" title="Releases">Releases</a> |
</p>

## Installing

`fv` can be installed [via Release][], [via Homebrew][], [via AUR][], [via
Scoop][] or [via Go][]:

[via Release]: #installing-via-release
[via Homebrew]: #installing-via-homebrew-macos-and-linux
[via AUR]: #installing-via-aur-arch-linux
[via Scoop]: #installing-via-scoop-windows
[via Go]: #installing-via-go

### Installing via Release

1. [Download a release for your platform][releases]
2. Extract the `fv` or `fv.exe` file from the `.tar.bz2` or `.zip` file
3. Move the extracted executable to somewhere on your `$PATH` (Linux/macOS) or
   `%PATH%` (Windows)

### Installing via Homebrew (macOS and Linux)

Install `fv` from the [`kenshaw/fv` tap][fv-tap] in the usual way with the [`brew`
command][homebrew]:

```sh
# install
$ brew install kenshaw/fv/fv
```

### Installing via AUR (Arch Linux)

Install `fv` from the [Arch Linux AUR][aur] in the usual way with the [`yay`
command][yay]:

```sh
# install
$ yay -S fv
```

Alternately, build and [install using `makepkg`][arch-makepkg]:

```sh
# clone package repo and make/install package
$ git clone https://aur.archlinux.org/fv.git && cd fv
$ makepkg -si
==> Making package: fv 0.12.10-1 (Fri 26 Aug 2022 05:56:09 AM WIB)
==> Checking runtime dependencies...
==> Checking buildtime dependencies...
==> Retrieving sources...
  -> Downloading fv-0.12.10.tar.gz...
...
```

### Installing via Scoop (Windows)

Install `fv` using [Scoop](https://scoop.sh):

```powershell
# Optional: Needed to run a remote script the first time
> Set-ExecutionPolicy RemoteSigned -Scope CurrentUser

# install scoop if not already installed
> irm get.scoop.sh | iex

# install fv with scoop
> scoop install fv
```

### Installing via Go

Install `fv` in the usual Go fashion:

```sh
# install latest fv version
$ go install github.com/kenshaw/fv@latest
```

## Usage

```sh
$ fv --help
fv, a command-line font viewer using terminal graphics

Usage:
  fv [flags] <font1> [font2, ..., fontN]

Flags:
      --all                    show all system fonts
      --bg color               background color (default rgba(255,255,255,0))
      --dpi int                dpi (default 100)
      --fg color               foreground color (default rgba(0,0,0,0))
  -h, --help                   help for fv
      --list                   list system fonts
      --margin int             margin (default 5)
      --match                  match system fonts
      --size int               font size (default 48)
      --style font-style       font style (default regular)
      --text string            text
      --variant font-variant   font variant (default normal)
  -v, --version                version for fv
```
