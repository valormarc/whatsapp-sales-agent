// Command agent runs the WhatsApp haggling sales agent webhook
// server. See the repository README for configuration and the
// environment variables it reads.
package main

import (
	"log"

	"github.com/valormarc/hackathon-agent/internal/server"
)

func main() {
	if err := server.Run(); err != nil {
		log.Fatal(err)
	}
}
