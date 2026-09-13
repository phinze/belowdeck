module github.com/phinze/belowdeck

go 1.25.5

// flake.nix pins a vendorHash over these modules. After changing anything
// here, run `nix build` and paste the new hash it reports into flake.nix
// before pushing, or the nix-config deploy fails on a hash mismatch.
require (
	github.com/ebitengine/purego v0.10.2
	github.com/hajimehoshi/ebiten/v2 v2.9.8
	github.com/prashantgupta24/mac-sleep-notifier v1.0.1
	github.com/spf13/cobra v1.10.2
	github.com/srwiley/oksvg v0.0.0-20221011165216-be6e8873101c
	github.com/srwiley/rasterx v0.0.0-20220730225603-2ab79fcdd4ef
	github.com/zalando/go-keyring v0.2.6
	golang.org/x/image v0.45.0
	gopkg.in/yaml.v3 v3.0.1
	rafaelmartins.com/p/streamdeck v0.0.0-20260905040856-709e442a380b
)

require rafaelmartins.com/p/usbhid v0.0.0-20260903160318-2edd824d3b06

require (
	al.essio.dev/pkg/shellescape v1.5.1 // indirect
	github.com/danieljoos/wincred v1.2.2 // indirect
	github.com/ebitengine/gomobile v0.0.0-20250923094054-ea854a63cce1 // indirect
	github.com/ebitengine/hideconsole v1.0.0 // indirect
	github.com/godbus/dbus/v5 v5.1.0 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/jezek/xgb v1.1.1 // indirect
	github.com/spf13/pflag v1.0.9 // indirect
	golang.org/x/net v0.0.0-20211118161319-6a13c67c3ce4 // indirect
	golang.org/x/sync v0.22.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
	golang.org/x/text v0.41.0 // indirect
)
