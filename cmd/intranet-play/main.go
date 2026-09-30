package main

import (
	"flag"
	"fmt"
	"intranet-play/internal/room"
	"log"
	"os"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: intranet-play <host|join>")
		os.Exit(1)
	}
	switch os.Args[1] {
	case "host":
		fs := flag.NewFlagSet("host", flag.ExitOnError)
		port := fs.Int("port", 9988, "port to listen on")
		name := fs.String("name", "Player 1", "name to use for the game")
		boardSize := fs.Int("boardSize", 10, "board size")
		_ = fs.Parse(os.Args[2:])
		if err := room.RunHost(*name, *port, *boardSize); err != nil {
			log.Fatal(err)
		}
	case "join":
		fs := flag.NewFlagSet("join", flag.ExitOnError)
		addr := fs.String("addr", "127.0.0.1:9988", "address to connect to")
		name := fs.String("name", "Player 2", "name to use for the game")
		_ = fs.Parse(os.Args[2:])
		if err := room.RunClient(*name, *addr); err != nil {
			log.Fatal(err)
		}
	case "help":
		fmt.Println("Usage: intranet-play <host|join>")
		fmt.Println("Options:")
		fmt.Println("  host -port int       port to listen on (default 9988)")
		fmt.Println("  host -name string    name to use for the game (default Player 1)")
		fmt.Println("  host -boardSize int  default board size 10-20 (default 10)")
		fmt.Println("  join -addr string    address to connect to (default 127.0.0.1:9988)")
		fmt.Println("  join -name string    name to use for the game (default Player 2)")
		os.Exit(1)
	default:
		fmt.Println("Usage: intranet-play <host|join>")
		os.Exit(1)
	}
}
