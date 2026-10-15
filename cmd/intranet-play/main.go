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
	case "bj":
		if len(os.Args) < 3 {
			fmt.Println("Usage: intranet-play bj <host|join>")
			os.Exit(1)
		}
		switch os.Args[2] {
		case "host":
			fs := flag.NewFlagSet("bj host", flag.ExitOnError)
			port := fs.Int("port", 9988, "port to listen on")
			name := fs.String("name", "Player 1", "name to use for the game")
			chips := fs.Int("chips", 100, "starting chips per player")
			seats := fs.Int("seats", 4, "max players at the table (2-6)")
			dealer := fs.String("dealer", "auto", "dealer mode: auto (2 players virtual, 3+ rotate) | rotate | virtual")
			_ = fs.Parse(os.Args[3:])
			if err := room.RunBjHost(*name, *port, *chips, *seats, *dealer); err != nil {
				log.Fatal(err)
			}
		case "join":
			fs := flag.NewFlagSet("bj join", flag.ExitOnError)
			addr := fs.String("addr", "127.0.0.1:9988", "address to connect to")
			name := fs.String("name", "Player 2", "name to use for the game")
			_ = fs.Parse(os.Args[3:])
			if err := room.RunBjClient(*name, *addr); err != nil {
				log.Fatal(err)
			}
		case "help":
			fmt.Println("Usage: intranet-play bj <host|join>")
			fmt.Println("Options:")
			fmt.Println("  host -port int      port to listen on (default 9988)")
			fmt.Println("  host -name string   name to use for the game (default Player 1)")
			fmt.Println("  host -chips int     starting chips per player (default 100)")
			fmt.Println("  host -seats int     max players at the table, 2-6 (default 4)")
			fmt.Println("  host -dealer string dealer mode: auto|rotate|virtual (default auto =")
			fmt.Println("                      2 人虚拟庄家，3 人以上轮流坐庄)")
			fmt.Println("  join -addr string   address to connect to (default 127.0.0.1:9988)")
			fmt.Println("  join -name string   name to use for the game (default Player 2)")
			fmt.Println("提示: 21 点人齐后房主输入 start；默认 3 人以上轮流坐庄、2 人虚拟庄家，")
			fmt.Println("      筹码输光即出局，只剩一人时整桌结束（restart 可重开）")
			os.Exit(1)
		default:
			fmt.Println("Usage: intranet-play bj <host|join>")
			os.Exit(1)
		}
	case "zjh":
		if len(os.Args) < 3 {
			fmt.Println("Usage: intranet-play zjh <host|join>")
			os.Exit(1)
		}
		switch os.Args[2] {
		case "host":
			fs := flag.NewFlagSet("zjh host", flag.ExitOnError)
			port := fs.Int("port", 9988, "port to listen on")
			name := fs.String("name", "Player 1", "name to use for the game")
			chips := fs.Int("chips", 100, "starting chips per player")
			ante := fs.Int("ante", 1, "ante per hand")
			maxStake := fs.Int("maxstake", 20, "max stake per hand")
			maxRounds := fs.Int("maxrounds", 6, "max betting rounds before you must compare or fold")
			seats := fs.Int("seats", 4, "max players at the table (2-6)")
			_ = fs.Parse(os.Args[3:])
			if err := room.RunZjhHost(*name, *port, *chips, *ante, *maxStake, *maxRounds, *seats); err != nil {
				log.Fatal(err)
			}
		case "join":
			fs := flag.NewFlagSet("zjh join", flag.ExitOnError)
			addr := fs.String("addr", "127.0.0.1:9988", "address to connect to")
			name := fs.String("name", "Player 2", "name to use for the game")
			_ = fs.Parse(os.Args[3:])
			if err := room.RunZjhClient(*name, *addr); err != nil {
				log.Fatal(err)
			}
		case "help":
			fmt.Println("Usage: intranet-play zjh <host|join>")
			fmt.Println("Options:")
			fmt.Println("  host -port int       port to listen on (default 9988)")
			fmt.Println("  host -name string    name to use for the game (default Player 1)")
			fmt.Println("  host -chips int      starting chips per player (default 100)")
			fmt.Println("  host -ante int       ante per hand (default 1)")
			fmt.Println("  host -maxstake int   max stake per hand (default 20)")
			fmt.Println("  host -maxrounds int  betting rounds before you must compare/fold (default 6)")
			fmt.Println("  host -seats int      max players at the table, 2-6 (default 4)")
			fmt.Println("  join -addr string    address to connect to (default 127.0.0.1:9988)")
			fmt.Println("  join -name string    name to use for the game (default Player 2)")
			fmt.Println("提示: 炸金花看牌才知道自己的牌；闷牌跟注半价、平局占优；人齐后房主输入 start")
			os.Exit(1)
		default:
			fmt.Println("Usage: intranet-play zjh <host|join>")
			os.Exit(1)
		}
	case "help":
		fmt.Println("Usage: intranet-play <host|join|ddz|bj|zjh>")
		fmt.Println("Options:")
		fmt.Println("  host -port int       port to listen on (default 9988)")
		fmt.Println("  host -name string    name to use for the game (default Player 1)")
		fmt.Println("  host -boardSize int  default board size 10-20 (default 10)")
		fmt.Println("  join -addr string    address to connect to (default 127.0.0.1:9988)")
		fmt.Println("  join -name string    name to use for the game (default Player 2)")
		fmt.Println("  ddz <host|join>      3 人斗地主（3 人局，房主占一个座位）")
		fmt.Println("  bj <host|join>       21 点（2-6 人，3 人以上轮流坐庄）")
		fmt.Println("  zjh <host|join>      炸金花（2-6 人，闷牌/看牌/比牌）")
		os.Exit(1)
	default:
		fmt.Println("Usage: intranet-play <host|join|ddz|bj|zjh>")
		os.Exit(1)
	}
}
