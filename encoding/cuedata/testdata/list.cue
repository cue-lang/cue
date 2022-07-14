import "list"

#aEnum: *"1" | "2" | "3"

aList: [...#aEnum] & [1,2,3,1,2,3]

aSqrExtra: [ for x in aList { x*x }, 100, 200]

aSqrEven: [ for x in aList if x rem 2 == 0 { x*x } ]