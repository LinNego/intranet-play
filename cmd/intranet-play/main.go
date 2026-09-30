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
	case "ddz":
		if len(os.Args) < 3 {
			fmt.Println("Usage: intranet-play ddz <host|join>")
			os.Exit(1)
		}
		switch os.Args[2] {
		case "host":
			fs := flag.NewFlagSet("ddz host", flag.ExitOnError)
			port := fs.Int("port", 9988, "port to listen on")
			name := fs.String("name", "Player 1", "name to use for the game")
			base := fs.Int("base", 1, "base score")
			rounds := fs.Int("rounds", 10, "rounds per match, 0 for unlimited")
			_ = fs.Parse(os.Args[3:])
			if err := room.RunDdzHost(*name, *port, *base, *rounds); err != nil {
				log.Fatal(err)
			}
		case "join":
			fs := flag.NewFlagSet("ddz join", flag.ExitOnError)
			addr := fs.String("addr", "127.0.0.1:9988", "address to connect to")
			name := fs.String("name", "Player 2", "name to use for the game")
			_ = fs.Parse(os.Args[3:])
			if err := room.RunDdzClient(*name, *addr); err != nil {
				log.Fatal(err)
			}
		case "help":
			fmt.Println("Usage: intranet-play ddz <host|join>")
			fmt.Println("Options:")
			fmt.Println("  host -port int      port to listen on (default 9988)")
			fmt.Println("  host -name string   name to use for the game (default Player 1)")
			fmt.Println("  host -base int      base score per round (default 1)")
			fmt.Println("  host -rounds int    rounds per match, 0 = unlimited (default 10)")
			fmt.Println("  join -addr string   address to connect to (default 127.0.0.1:9988)")
			fmt.Println("  join -name string   name to use for the game (default Player 2)")
			fmt.Println("提示: 斗地主是 3 人局，房主自己占一个座位，另外两人各 join 一次")
			os.Exit(1)
		default:
			fmt.Println("Usage: intranet-play ddz <host|join>")
			os.Exit(1)
		}
	case "help":
		fmt.Println("Usage: intranet-play <host|join|ddz>")
		fmt.Println("Options:")
		fmt.Println("  host -port int       port to listen on (default 9988)")
		fmt.Println("  host -name string    name to use for the game (default Player 1)")
		fmt.Println("  host -boardSize int  default board size 10-20 (default 10)")
		fmt.Println("  join -addr string    address to connect to (default 127.0.0.1:9988)")
		fmt.Println("  join -name string    name to use for the game (default Player 2)")
		fmt.Println("  ddz <host|join>      3 人斗地主（3 人局，房主占一个座位）")
		os.Exit(1)
	default:
		fmt.Println("Usage: intranet-play <host|join|ddz>")
		os.Exit(1)
	}
}
