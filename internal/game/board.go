package game

import "fmt"

type Board struct {
	Cells  [][]int // 0: empty, 1: host, 2: guest , 3: host_win, 4: guest_win
	Size   int
	Winner int
}

type GameStatus string

const (
	GameStatusPending  GameStatus = "PENDING"
	GameStatusWin      GameStatus = "WIN"
	GameStatusDraw     GameStatus = "DRAW"
	GameStatusContinue GameStatus = "CONTINUE"
)

func NewBoard(size int) *Board {
	cells := make([][]int, size)
	for i := range cells {
		cells[i] = make([]int, size)
		for j := range cells[i] {
			cells[i][j] = 0
		}
	}
	return &Board{Cells: cells, Size: size}
}

func (b *Board) Place(player, x, y int) (GameStatus, error) {
	if err := b.checkPosition(x, y); err != nil {
		return GameStatusContinue, err
	}
	b.Cells[x][y] = player
	if b.checkWin(x, y, player) {
		return GameStatusWin, nil
	}
	if b.IsFull() {
		return GameStatusDraw, nil
	}
	return GameStatusContinue, nil
}

func (b *Board) IsFull() bool {
	for i := range b.Cells {
		for j := range b.Cells[i] {
			if b.Cells[i][j] == 0 {
				return false
			}
		}
	}
	return true
}

func (b *Board) checkPosition(x, y int) error {
	if x < 0 || x >= b.Size || y < 0 || y >= b.Size {
		return fmt.Errorf("invalid position: (%d, %d)", x, y)
	}
	if b.Cells[x][y] != 0 {
		return fmt.Errorf("cell is not empty: (%d, %d)", x, y)
	}
	return nil
}

func (b *Board) checkWin(x, y, player int) bool {
	directions := [4][2]int{
		{0, 1},  // horizontal
		{1, 0},  // vertical
		{1, 1},  // diagonal
		{1, -1}, // anti-diagonal
	}
	n := b.Size
	for _, d := range directions {
		count := 1
		for i, j := x+d[0], y+d[1]; i >= 0 && i < n && j >= 0 && j < n && b.Cells[i][j] == player; i, j = i+d[0], j+d[1] {
			count++
		}
		for i, j := x-d[0], y-d[1]; i >= 0 && i < n && j >= 0 && j < n && b.Cells[i][j] == player; i, j = i-d[0], j-d[1] {
			count++
		}
		if count >= 5 {
			b.Cells[x][y] = player + 2
			for i, j := x+d[0], y+d[1]; i >= 0 && i < n && j >= 0 && j < n && b.Cells[i][j] == player; i, j = i+d[0], j+d[1] {
				b.Cells[i][j] = player + 2
			}
			for i, j := x-d[0], y-d[1]; i >= 0 && i < n && j >= 0 && j < n && b.Cells[i][j] == player; i, j = i-d[0], j-d[1] {
				b.Cells[i][j] = player + 2
			}
			b.Winner = player
			return true
		}
	}
	return false
}
