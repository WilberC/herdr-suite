# Theme

Back to the [README](../README.md).

The switcher renders with a theme from [tui-kit](https://github.com/WilberC/tui-kit),
resolved in this order:

1. **`HS_THEME`** — names a JSON file in the themes directory (`$HS_THEME=gruvbox`
   loads `gruvbox.json`), under `~/.config/hs/themes` (or your platform's config
   dir). The file is a partial override over the built-in Tokyo Night palette:

   ```json
   { "name": "gruvbox", "colors": { "text": "#ebdbb2", "accent": 196, "chip": "" } }
   ```

2. **No setting** — the terminal is asked for its own colours (foreground,
   background, the sixteen ANSI slots) and the theme is generated from the
   answer, so the switcher follows whatever theme your terminal wears.

Every failure — a missing or broken file, a terminal that cannot be asked —
falls back to the built-in Tokyo Night palette. A role set to `""` uses the
terminal's own default.
