package render

import "github.com/jnnngs/h3270/internal/host"

// Renderer renders a 3270 screen to a string format (HTML, Text, etc).
type Renderer interface {
	Render(screen *host.Screen, actionURL, id string) string
}
