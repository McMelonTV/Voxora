package types

/*
#include <stdint.h>
#include "libvoxora_types.h"
*/
import "C"

type GOThing struct {
	A uint32
	B byte
}

func CThingToGOThing(cThing C.Thing) GOThing {
	return GOThing{
		A: uint32(cThing.a),
		B: byte(cThing.b),
	}
}

func GOThingToCThing(goThing GOThing) C.Thing {
	return C.Thing{
		a: C.uint32_t(goThing.A),
		b: C.char(goThing.B),
	}
}
