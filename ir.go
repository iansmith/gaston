package main

import "fmt"

// IROpCode is a three-address IR instruction opcode.
type IROpCode int

const (
	IRAdd  IROpCode = iota // Dst = Src1 + Src2
	IRSub                  // Dst = Src1 - Src2
	IRMul                  // Dst = Src1 * Src2
	IRDiv                  // Dst = Src1 / Src2
	IRMod                  // Dst = Src1 % Src2
	IRBitAnd               // Dst = Src1 & Src2
	IRBitOr                // Dst = Src1 | Src2
	IRBitXor               // Dst = Src1 ^ Src2
	IRBitNot               // Dst = ~Src1
	IRShl                  // Dst = Src1 << Src2
	IRShr                  // Dst = Src1 >> Src2 (arithmetic)
	IRCopy                 // Dst = Src1
	IRLoad                 // Dst = Src1[Src2]   (array element load)
	IRStore                // Dst[Src1] = Src2   (array element store)
	IRGetAddr              // Dst = &Src1        (base address of array)
	IRLabel                // Extra: label name
	IRJump                 // goto Extra
	IRJumpT                // if Src1 != 0 goto Extra
	IRJumpF                // if Src1 == 0 goto Extra
	IRLt                   // Dst = (Src1 < Src2)
	IRLe                   // Dst = (Src1 <= Src2)
	IRGt                   // Dst = (Src1 > Src2)
	IRGe                   // Dst = (Src1 >= Src2)
	IREq                   // Dst = (Src1 == Src2)
	IRNe                   // Dst = (Src1 != Src2)
	IRParam                // push Src1 as next call argument
	IRCall                 // Dst = Extra(Src1.IVal args)
	IRReturn               // return Src1 (Src1.Kind==AddrNone → void)
	IREnter                // function entry marker; Extra = name
	IRStrAddr              // Dst = address of string literal; Extra = label
	IRDerefLoad            // Dst = *Src1  (load 8 bytes via pointer)
	IRDerefStore           // *Dst = Src1  (store 8 bytes via pointer)

	// Unsigned integer operations (operands treated as unsigned).
	IRUDiv  // Dst = Src1 / Src2   (unsigned divide)
	IRUMod  // Dst = Src1 % Src2   (unsigned modulo)
	IRUShr  // Dst = Src1 >> Src2  (logical/unsigned shift right)
	IRULt   // Dst = (Src1 <  Src2) unsigned
	IRULe   // Dst = (Src1 <= Src2) unsigned
	IRUGt   // Dst = (Src1 >  Src2) unsigned
	IRUGe   // Dst = (Src1 >= Src2) unsigned
)

// AddrKind identifies what an IR address refers to.
type AddrKind int

const (
	AddrNone   AddrKind = iota // unused / void
	AddrConst                  // integer constant (IVal)
	AddrTemp                   // compiler-generated temporary (Name = "tN")
	AddrLocal                  // local variable or parameter (Name = identifier)
	AddrGlobal                 // global variable (Name = identifier)
)

// IRAddr is one operand in a three-address IR instruction.
type IRAddr struct {
	Kind AddrKind
	IVal int    // for AddrConst
	Name string // for AddrTemp, AddrLocal, AddrGlobal
}

func (a IRAddr) String() string {
	switch a.Kind {
	case AddrNone:
		return "_"
	case AddrConst:
		return fmt.Sprintf("%d", a.IVal)
	default:
		return a.Name
	}
}

// Quad is one three-address IR instruction.
type Quad struct {
	Op    IROpCode
	Dst   IRAddr
	Src1  IRAddr
	Src2  IRAddr
	Extra string // label name (IRLabel/IRJump/IRJumpT/IRJumpF) or function name (IRCall/IREnter)
}

// IRGlobal describes one global variable declaration.
type IRGlobal struct {
	Name       string
	IsArr      bool
	IsPtr      bool // true for TypeIntPtr or TypeCharPtr globals
	IsExtern   bool // true for extern-declared globals (no storage allocated)
	Size       int  // 1 for scalar, N for array[N]
	HasInitVal bool
	InitVal    int // constant initializer value (only when HasInitVal && !IsArr)
}

// IRLocal describes one local variable in a function (not a parameter).
type IRLocal struct {
	Name    string
	IsArray bool
	IsPtr   bool // true for TypeIntPtr or TypeCharPtr locals
	ArrSize int  // 1 for scalar, N for int x[N]
}

// IRFunc is the IR for one function.
type IRFunc struct {
	Name      string
	Params    []string   // parameter names in declaration order
	ParamType []TypeKind // corresponding types
	Locals    []IRLocal  // local variables declared inside the function body
	Quads     []Quad
}

// IRStrLit is one string literal in the rodata section.
type IRStrLit struct {
	Label   string // synthetic label (e.g. "str0")
	Content string // string content (without NUL terminator; NUL is appended at output)
}

// IRProgram is the complete IR for a program.
type IRProgram struct {
	Globals []IRGlobal
	Funcs   []*IRFunc
	StrLits []IRStrLit // string literals (rodata)
}
