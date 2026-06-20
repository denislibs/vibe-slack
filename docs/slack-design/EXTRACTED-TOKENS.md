# Slack design system — extracted 1:1 (live, documentolog workspace)

Source: `app.slack.com` (logged-in), default **light theme + aubergine sidebar**, viewport 1440×900.
Captured via computed styles + the 770 `--dt_*`/`--p-*` design-token CSS variables. These are the
authoritative values to replicate in the client.

## Brand / core colors
| token | value |
|---|---|
| aubergine (sidebar) | `#4a154b` (sidebar nav uses `#3F0E40`) |
| black | `#1d1c1d` |
| slack-blue | `#36c5f0` |
| slack-green | `#2eb67d` |
| slack-red | `#e01e5a` |
| slack-yellow | `#ecb22e` |
| berry | `#5e1237` |
| bright-aubergine | `#611f69` |

## Text (content) colors — light surfaces
| role | value |
|---|---|
| primary text | `#1d1c1d` |
| secondary text | `#454447` |
| tertiary / muted | `#5e5d60` |
| link (hgl-1) | `#1264a3` |
| success/green accent (hgl-2) | `#007a5a` |
| danger/important | `#c01343` (outline `#e01e5a`) |

## Surfaces
| role | value |
|---|---|
| base primary (main bg) | `#ffffff` |
| base secondary | `#f8f8f8` |
| base tertiary | `#eaeaea` |
| border | `#eaeaea` |
| outline primary | `#7c7a7f` |
| hover (on white) | `rgba(69,68,71,.06)` (#4544470f) |
| pressed (on white) | `rgba(69,68,71,.13)` (#45444721) |

## Sidebar theme (aubergine — `--p-team_sidebar__*`)
| role | value |
|---|---|
| nav / sidebar bg | `#3F0E40` |
| sidebar text | `#FFFFFF` (rows use `rgba(246,228,255,.9)`) |
| selected row bg | `#f9edff` (aubergine-0) |
| selected row text | `#39063a` (aubergine-100) |
| hover row | `rgba(255,255,255,.1)` |
| badge bg / text | `#ECE7EC` / `#3F0E40` |
| rail bg | `#3F0E40` (same family) |

## Typography
- Family (default): `"Slack-Lato", "Slack-Fractions", "appleLogo", sans-serif`
  → **Slack-Lato ≈ Lato (SIL OFL)**; use Lato + system fallback:
  `"Lato", system-ui, -apple-system, "Segoe UI", Roboto, "Helvetica Neue", Arial, sans-serif`
- Monospace: `"Monaco","Menlo","Consolas","Courier New",monospace`
- Sizes: micro **12**, caption **13**, base **15**, subtitle **18**, title **22**, headline **28** (px)
- Weights: base **400**, semibold **600**, bold **700**, black **900**
- Line-height: base **1.5**, small **1.25** (body computed ≈ 22px on 15px)

## Spacing scale (`--dt_static_space-*`)
2, 4, 8, 12, 16, 20, 24, 28, 32, 36, 40, 44, 48 … (px)

## Radii (`--dt_static_radius-*`)
small **2px**, base **4px**, large **8px**, xlarge **12px**, rounded **9999px**; sidebar rows **6px**

## Shadows (`--dt_static_shadow-*`)
- sm: `0 1px 2px 0 #0000000c`
- base: `0 1px 3px 0 #00000019, 0 1px 2px -1px #00000019`
- md: `0 2px 4px -2px #00000019, 0 4px 6px -1px #00000019`
- lg: `0 4px 6px -4px #00000019, 0 10px 15px -3px #00000019`
- xl: `0 20px 25px -5px #00000019, 0 8px 10px -6px #00000019`

## Transitions
- duration short **80ms**, long **.16s**
- ease default `cubic-bezier(.36,.19,.29,1)`

## Layout geometry (measured, 1440px window)
| region | metric |
|---|---|
| workspace rail | width **70px**, padding-top 8px, bg `#3F0E40` |
| channel sidebar | width **~328px**, bg `#3F0E40` |
| channel row | height **28px**, radius **6px**, padding-left **24px**, padding-right 8px, font 15px/lh 28px |
| section header ("Channels") | row height 28px |
| topbar / view header | height **49px**, bg `#fff`, padding-left **18px**, padding-right **12px** |
| channel title | 18px / weight 900 (Slack convention) |
| main view | white bg, flexible width |
| composer | horizontal inset **20px**; input box radius 8px; cards radius 12px |

## Buttons & inputs (measured)
| element | spec |
|---|---|
| primary button (filled) | bg `#007a5a`, hover `#148567`, text `#fff`, weight 700, radius **4px**, small h **28px** / medium h **36px**, padding `0 12px` |
| outline button (small) | h **28px**, padding `0 12px 1px`, font **13px/700**, text `#1d1c1d`, bg `#fff`, border `1px solid rgba(94,93,96,.45)`, radius **4px** |
| icon button (medium) | **32px** square, icon 20px, radius **8px**, transparent bg, hover surface tint |
| topbar search | h **28px**, padding `5px 12px`, font 15px, radius **6px** (transparent on aubergine, white text) |
| text input (`c-input_text`) | border `1px solid rgba(29,28,29,.3)`, radius **4px**, padding `0 12px`, h ~36px, font 15px |
| **focus ring (a11y)** | `box-shadow: 0 0 0 1px #1264a3, 0 0 0 5px #0e9dd333` (`--dt_static_shadow-a11y`) |
| composer editor | min-height **38px**, padding **8px 12px**, font 15px / lh 22px |
| modal overlay | scrim `rgba(29,28,29,.7)`; modal radius ~8px, shadow `--dt_static_shadow-xl` |

## Notes / pending
- Message-row metrics (40px avatar gutter, 16px message left pad, hover bg) to be measured on a
  populated channel.
- Slack-Lato/Slack-Fractions are proprietary woff2 on slack-edge CDN; replicate with **Lato** (open).
- Full raw 770-token dump available by re-running the extractor in `app.slack.com`.
