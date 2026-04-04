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
	IRFDerefLoad           // Dst = *Src1  (load 8-byte double via pointer; result is FP)
	IRFDerefStore          // *Dst = Src1  (store 8-byte double via pointer; Src1 is FP)
	IRCharLoad             // Dst = Src1[Src2]  (char* subscript — byte load, no scaling)
	IRCharStore            // Dst[Src1] = Src2  (char* subscript — byte store, no scaling)

	// Unsigned integer operations (operands treated as unsigned).
	IRUDiv  // Dst = Src1 / Src2   (unsigned divide)
	IRUMod  // Dst = Src1 % Src2   (unsigned modulo)
	IRUShr  // Dst = Src1 >> Src2  (logical/unsigned shift right)
	IRULt   // Dst = (Src1 <  Src2) unsigned
	IRULe   // Dst = (Src1 <= Src2) unsigned
	IRUGt   // Dst = (Src1 >  Src2) unsigned
	IRUGe   // Dst = (Src1 >= Src2) unsigned

	// Floating-point operations (64-bit double precision).
	IRFAdd        // Dst = Src1 + Src2  (double)
	IRFSub        // Dst = Src1 - Src2  (double)
	IRFMul        // Dst = Src1 * Src2  (double)
	IRFDiv        // Dst = Src1 / Src2  (double)
	IRFNeg        // Dst = -Src1        (double)
	IRFCopy       // Dst = Src1         (FP copy via D registers)
	IRFLt         // Dst = (Src1 <  Src2) → int (ordered, false for NaN)
	IRFLe         // Dst = (Src1 <= Src2) → int (ordered, false for NaN)
	IRFGt         // Dst = (Src1 >  Src2) → int (ordered, false for NaN)
	IRFGe         // Dst = (Src1 >= Src2) → int (ordered, false for NaN)
	IRFEq         // Dst = (Src1 == Src2) → int (false for NaN)
	IRFNe         // Dst = (Src1 != Src2) → int (true  for NaN)
	IRIntToDouble // Dst = (double)Src1   (int64 → double)
	IRDoubleToInt // Dst = (int64)Src1    (double → int64, truncate toward zero)
	IRFParam      // push Src1 as next FP call argument (into D0-D7)

	// Struct field operations.
	// IRFieldLoad:  Dst = *(Src1 + Src2.IVal)  — load field at byte offset Src2.IVal
	// IRFieldStore: *(Dst + Src2.IVal) = Src1  — store value Src1 at field byte offset Src2.IVal
	IRFieldLoad   // Dst = field load (int): base ptr in Src1, byte offset in Src2.IVal
	IRFieldStore  // field store (int): base ptr in Dst, value in Src1, byte offset in Src2.IVal
	IRFFieldLoad  // Dst = field load (double): base ptr in Src1, byte offset in Src2.IVal
	IRFFieldStore // field store (double): base ptr in Dst, value in Src1, byte offset in Src2.IVal

	// IRAddrOf: Dst = storage-address of Src1 — always SP/FP+offset for locals,
	// global VA for globals. Unlike IRGetAddr, never loads through a pointer slot.
	// Used exclusively by KindAddrOf (&var) in irgen.go.
	IRAddrOf

	// Variable-length array allocation.
	// IRVLAAlloc: Dst = alloca(Src1 * 8) — allocate Src1 elements on the stack,
	// store the base pointer in the frame slot Dst.  The function must use
	// FP-relative addressing for all static frame slots once any VLA has been
	// allocated (SP may differ from FP after this point).
	IRVLAAlloc

	// Integer promotion (C usual arithmetic conversions).
	// Src2.IVal is the bit width (8 for char, 16 for short).
	// IRSignExtend: Dst = sign_extend(Src1, Src2.IVal) — signed promotion to 64-bit int
	// IRZeroExtend: Dst = zero_extend(Src1, Src2.IVal) — unsigned promotion to 64-bit int
	IRSignExtend
	IRZeroExtend

	// Function pointer operations.
	// IRFuncAddr:    Dst = VA of function named Extra (for fp = funcname; assignments)
	// IRFuncPtrCall: Dst = (*Src1)(args); Src1 holds the function pointer value
	IRFuncAddr
	IRFuncPtrCall
)

// AddrKind identifies what an IR address refers to.
type AddrKind int

const (
	AddrNone   AddrKind = iota // unused / void
	AddrConst                  // integer constant (IVal)
	AddrTemp                   // compiler-generated temporary (Name = "tN")
	AddrLocal                  // local variable or parameter (Name = identifier)
	AddrGlobal                 // global variable (Name = identifier)
	AddrFConst                 // floating-point constant (FVal = value, Name = pool label)
)

// IRAddr is one operand in a three-address IR instruction.
type IRAddr struct {
	Kind AddrKind
	IVal int     // for AddrConst
	FVal float64 // for AddrFConst
	Name string  // for AddrTemp, AddrLocal, AddrGlobal, AddrFConst (pool label)
}

func (a IRAddr) String() string {
	switch a.Kind {
	case AddrNone:
		return "_"
	case AddrConst:
		return fmt.Sprintf("%d", a.IVal)
	case AddrFConst:
		return fmt.Sprintf("%g(%s)", a.FVal, a.Name)
	default:
		return a.Name
	}
}

// Quad is one three-address IR instruction.
type Quad struct {
	Op       IROpCode
	Dst      IRAddr
	Src1     IRAddr
	Src2     IRAddr
	Extra    string   // label name (IRLabel/IRJump/IRJumpT/IRJumpF) or function name (IRCall/IREnter)
	TypeHint TypeKind // for IRFieldLoad/IRFieldStore: the field's TypeKind (drives byte/halfword/word insn)
}

// IRGlobal describes one global variable declaration.
type IRGlobal struct {
	Name       string
	IsArr      bool
	IsPtr      bool   // true for TypeIntPtr or TypeCharPtr globals
	IsStruct   bool   // true for TypeStruct globals
	StructTag  string // struct type name (when IsStruct)
	IsExtern   bool   // true for extern-declared globals (no storage allocated)
	Size       int    // 1 for scalar, N for array[N] or struct (N = numFields)
	InnerDim   int    // inner dimension for 2D arrays (0 for 1D or non-array)
	HasInitVal bool
	InitVal    int // constant initializer value (only when HasInitVal && !IsArr)
}

// IRLocal describes one local variable in a function (not a parameter).
type IRLocal struct {
	Name      string
	IsArray   bool
	IsPtr     bool   // true for TypeIntPtr or TypeCharPtr locals
	IsStruct  bool   // true for TypeStruct locals
	IsVLA     bool   // true for variable-length array (runtime size; pointer slot in frame)
	StructTag string // struct type name (when IsStruct)
	ArrSize   int    // 1 for scalar, N for int x[N]; for struct: number of fields; 0 for VLA
}

// IRFunc is the IR for one function.
type IRFunc struct {
	Name       string
	ReturnType TypeKind   // return type (TypeVoid if void)
	Params     []string   // parameter names in declaration order (no "..." marker)
	ParamType  []TypeKind // corresponding types
	IsVariadic bool       // true if this function accepts variadic arguments
	HasVLA     bool       // true if any local is a VLA (requires FP-relative frame addressing)
	Locals     []IRLocal  // local variables declared inside the function body
	Quads      []Quad
}

// IRStrLit is one string literal in the rodata section.
type IRStrLit struct {
	Label   string // synthetic label (e.g. "str0")
	Content string // string content (without NUL terminator; NUL is appended at output)
}

// IRFConst is one floating-point constant in the literal pool.
type IRFConst struct {
	Label string  // synthetic pool label (e.g. "fc0")
	Value float64 // IEEE 754 double value
}

// IRProgram is the complete IR for a program.
type IRProgram struct {
	Globals    []IRGlobal
	Funcs      []*IRFunc
	StrLits    []IRStrLit // string literals (rodata)
	FConsts    []IRFConst // floating-point constants (literal pool entries)
	FuncRefs   []string   // names of user functions whose addresses are taken (IRFuncAddr)
	StructDefs map[string]*StructDef // struct type definitions (from ast.go)
}
