package render

import "github.com/jnnngs/3270Web/internal/host"

// Renderer renders a 3270 screen to a string format (HTML, Text, etc).
type Renderer interface {
	Render(screen *host.Screen, actionURL, id string) string
}
