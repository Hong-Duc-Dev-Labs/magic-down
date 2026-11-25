package main

import (
	"flag"
	"log"

	"magicdown/pkg/ipc"
	"magicdown/pkg/nativemessaging"
)

func main() {
	port := flag.Int("port", ipc.DefaultPort, "MagicDown desktop app IPC port")
	flag.Parse()

	if err := nativemessaging.RunHostWithStdio(*port); err != nil {
		log.Fatalf("native messaging host error: %v", err)
	}
}
