module example.org/imports/a

go 1.26

require (
	example.org/imports/b v0.0.0
	example.org/imports/c v0.0.0
)

replace (
	example.org/imports/b => ../b
	example.org/imports/c => ../c
)
