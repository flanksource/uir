package sample

func Large(first, second, third, fourth int) int {
	return first + second + third + fourth
}

func Caller() int {
	return Large(1, 2, 3, 4)
}
