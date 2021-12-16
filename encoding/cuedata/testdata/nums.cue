import (
	"strings"
	"math"
)

constant:  2
several:   1 | 2 | 3 | 4
inclusive: >=2 & <=3
exclusive: int & >2 & <3
multi:     int & >=2 & <=3 | strings.MaxRunes(5)
legacy:    >2 & <3
cents:     math.MultipleOf(0.05)
neq: !=4