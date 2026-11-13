# Ask UI/motion review

The first Ask pass was not critiqued with `https://github.com/emilkowalski/skills`.
This pass used the published guidance from:

- `skills/emil-design-eng/SKILL.md`
- `skills/review-animations/SKILL.md`

The guidance was applied to the native MyGO surface rather than copied as web
CSS.

## Findings and changes

| Finding | Decision |
| --- | --- |
| Ask opens from a keyboard shortcut and is used many times per day. | No panel entrance animation. The panel appears immediately; adding a transition here would delay a high-frequency action. |
| Streaming text is already a continuous state change. | No per-token animation or staggered answer blocks. Deltas paint as they arrive. |
| History is a transient menu anchored to a button. | Reuse the existing `panelIn` surface: one 170ms fade/8px rise, no loop at rest. It is not a modal. |
| Long chat titles and previews can overflow a 400px panel. | Clip titles and previews to one line; keep the title in the anchored history surface. |
| Sources were only available in a separate block below the answer. | Render Markdown and turn verified `[E1]` tokens into inline links. Unknown tokens remain plain text. |
| A cited note may change after the answer. | Cache freshness at refresh time and mark the source `· changed`, with an explicit warning. Do not animate stale state. |
| A model connection result was hidden inside the settings modal. | Keep the last test result in the panel, with success/error text. |
| History actions could compete with a live run. | Disable new-chat/switch/delete actions while a run is active and surface a clear error. |
| Reduced-motion/media-query guidance is web-specific. | The native surface has no independent animation loop; the only added motion uses the app's existing surface primitive. |

## Verdict

The Ask panel stays crisp and responsive: high-frequency open/close is instant,
streaming is immediate, and only the intentional history popover uses the app's
existing short surface entrance. No decorative motion was added.
