package game

import "testing"

func TestPlaceAndHorizontalWin(t *testing.T) {
	b := NewBoard(15)
	for x := 0; x < 4; x++ {
		if _, err := b.Place(1, x, 7); err != nil {
			t.Fatal(err)
		}
		if _, err := b.Place(2, x, 8); err != nil {
			t.Fatal(err)
		}
	}
	st, err := b.Place(1, 4, 7)
	if err != nil {
		t.Fatal(err)
	}
	if st != GameStatusWin {
		t.Fatalf("want WIN, got %s", st)
	}
}
func TestPlaceAndVerticalWin(t *testing.T) {
	b := NewBoard(15)
	for y := 0; y < 4; y++ {
		if _, err := b.Place(1, 7, y); err != nil {
			t.Fatal(err)
		}
		if _, err := b.Place(2, 8, y); err != nil {
			t.Fatal(err)
		}
	}
	st, err := b.Place(1, 7, 4)
	if err != nil {
		t.Fatal(err)
	}
	if st != GameStatusWin {
		t.Fatalf("want WIN, got %s", st)
	}
}
func TestPlaceAndDiagonalWin(t *testing.T) {
	b := NewBoard(15)
	for x := 0; x < 4; x++ {
		if _, err := b.Place(1, x, x); err != nil {
			t.Fatal(err)
		}
	}
	for x := 0; x < 4; x++ {
		if _, err := b.Place(2, x, 14-x); err != nil {
			t.Fatal(err)
		}
	}
	st, err := b.Place(1, 4, 4)
	if err != nil {
		t.Fatal(err)
	}
	if st != GameStatusWin {
		t.Fatalf("want WIN, got %s", st)
	}
}
func TestPlaceAndAntiDiagonalWin(t *testing.T) {
	b := NewBoard(15)
	for x := 0; x < 4; x++ {
		if _, err := b.Place(1, x, 14-x); err != nil {
			t.Fatal(err)
		}
	}
	for x := 0; x < 4; x++ {
		if _, err := b.Place(2, x, x); err != nil {
			t.Fatal(err)
		}
	}
	st, err := b.Place(1, 4, 10)
	if err != nil {
		t.Fatal(err)
	}
	if st != GameStatusWin {
		t.Fatalf("want WIN, got %s", st)
	}
}
func TestPlaceAndFullBoard(t *testing.T) {
	b := NewBoard(4) // size < 5 => draw possible without a win
	var lastSt GameStatus
	var err error
	for x := 0; x < 4; x++ {
		for y := 0; y < 4; y++ {
			player := 1
			if (x+y)%2 == 1 {
				player = 2
			}
			lastSt, err = b.Place(player, x, y)
			if err != nil {
				t.Fatalf("place (%d,%d): %v", x, y, err)
			}
			if lastSt == GameStatusWin {
				t.Fatalf("unexpected win at (%d,%d)", x, y)
			}
		}
	}
	if lastSt != GameStatusDraw {
		t.Fatalf("want DRAW, got %s", lastSt)
	}
}
func TestPlaceAndContinue(t *testing.T) {
	b := NewBoard(15)
	if _, err := b.Place(1, 7, 7); err != nil {
		t.Fatal(err)
	}
	st, err := b.Place(2, 7, 8)
	if err != nil {
		t.Fatal(err)
	}
	if st != GameStatusContinue {
		t.Fatalf("want CONTINUE, got %s", st)
	}
}
func TestInvalidPosition(t *testing.T) {
	b := NewBoard(15)
	if _, err := b.Place(1, -1, 0); err == nil {
		t.Fatal("want error for out of range")
	}
	_, _ = b.Place(1, 0, 0)
	if _, err := b.Place(2, 0, 0); err == nil {
		t.Fatal("want error for occupied")
	}
}
