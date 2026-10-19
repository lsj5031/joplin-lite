# Vendored fonts

Overpass and Overpass Mono, from [google/fonts](https://github.com/google/fonts)
(`ofl/overpass`, `ofl/overpassmono`), the families DESIGN.md specifies. Both are
variable fonts (weight axis); they are registered by `shell/design.go` with
`ui.RegisterFont` so the native shell draws in the same voice as the web build,
which self-hosts the same families as WOFF2 under `web/public/fonts`.

Licensed under the SIL Open Font License 1.1 — see [OFL.txt](OFL.txt).

Refresh with:

```sh
curl -o shell/fonts/Overpass.ttf \
  "https://raw.githubusercontent.com/google/fonts/main/ofl/overpass/Overpass%5Bwght%5D.ttf"
curl -o shell/fonts/OverpassMono.ttf \
  "https://raw.githubusercontent.com/google/fonts/main/ofl/overpassmono/OverpassMono%5Bwght%5D.ttf"
```
