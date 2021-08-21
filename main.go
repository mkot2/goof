package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"
)

const (
	ADD_SUB byte = iota
	PTR_MOV
	JMP_ZER
	JMP_NOT_ZER
	PUT_CHR
	RAD_CHR
	CLR
	MUL_CPY
	SCN_RGT
	SCN_LFT
)

type Instruction struct {
	Type    byte
	Data    int
	AuxData int
}

var filename string
var memorySize int
var trackStatistics bool

var instructionCount int
var optInstructionCount int
var stringLength int
var preprocessorTime time.Duration
var interpreterTime time.Duration

func elapsed(what int) func() {
	var start = time.Now()
	return func() {
		switch what {
		case 0:
			preprocessorTime = time.Since(start)
		case 1:
			interpreterTime = time.Since(start)
		}
	}
}

func fold(code string, i *int, char byte) int {
	var count = 1
	for *i < stringLength-1 && (code)[*i+1] == char {
		count++
		*i++
	}

	return count
}

func processBalanced(s string, char1 string, char2 string) string {
	var total = strings.Count(s, char1) - strings.Count(s, char2)
	if total > 0 {
		return strings.Repeat(char1, total)
	} else if total < 0 {
		return strings.Repeat(char2, -total)
	} else {
		return ""
	}
}

func compile(code string) *[]Instruction {
	defer elapsed(0)()
	// Optimize
	// Remove useless characters
	var dummyChars = regexp.MustCompile(`[^\+\-\>\<\.\,\]\[]`)
	code = dummyChars.ReplaceAllString(code, "")

	// Remove NOPs
	var nopAddSub = regexp.MustCompile(`[+-]{2,}`)
	var nopRgtLft = regexp.MustCompile(`[><]{2,}`)
	code = nopAddSub.ReplaceAllStringFunc(code, func(s string) string { return processBalanced(s, "+", "-") })
	code = nopRgtLft.ReplaceAllStringFunc(code, func(s string) string { return processBalanced(s, ">", "<") })

	// Clearloop optimization
	var clearloop = regexp.MustCompile(`[+-]*(?:\[[+-]+\])+`) // Also delete any modifications to cell that is being cleared
	code = clearloop.ReplaceAllString(code, "C")

	// Scanloop optimization
	var scanloopCounter int
	var scanloopMap = make([]int, 0)
	var scanloopRight = regexp.MustCompile(`\[>+\]`)
	var scanloopLeft = regexp.MustCompile(`\[<+\]`)
	code = scanloopRight.ReplaceAllStringFunc(code, func(s string) string {
		scanloopMap = append(scanloopMap, strings.Count(s, ">"))
		return "R"
	})
	code = scanloopLeft.ReplaceAllStringFunc(code, func(s string) string {
		scanloopMap = append(scanloopMap, strings.Count(s, "<"))
		return "L"
	})

	// Don't clear or print if cell is known zero
	var noClearPrint = regexp.MustCompile(`[RL]+C|[CRL]+\.+`)
	code = noClearPrint.ReplaceAllString(code, "")

	// Don't update cells if they are immediately overwritten by stdin
	var overwrite = regexp.MustCompile(`[+-C]+,`)
	code = overwrite.ReplaceAllString(code, ",")

	// Basic multiloops/copyloops optimization (won't optimize more complicated ones for now)
	var copyloopCounter int
	var copyloopMap = make([]int, 0)
	var copyloopMulMap = make([]int, 0)
	var copyloop = regexp.MustCompile(`\[-[<>]+\++[<>]+\]`)
	code = copyloop.ReplaceAllStringFunc(code, func(s string) string {
		var balancedMove = strings.Count(s, ">")-strings.Count(s, "<") == 0
		if balancedMove {
			var tempRegex = regexp.MustCompile(`[<>]+`).FindString(s)
			copyloopMap = append(copyloopMap, -strings.Count(tempRegex, "<")+strings.Count(tempRegex, ">"))
			copyloopMulMap = append(copyloopMulMap, strings.Count(s, "+"))
			return "P"
		} else {
			return s
		}
	})

	// Compile & link loops
	stringLength = len(code)
	var instructions = make([]Instruction, 0)
	var tBraceStack = make([]int, 0)
	for i := 0; i < stringLength; i++ {
		var newInstruction Instruction
		switch code[i] {
		case '+':
			newInstruction = Instruction{ADD_SUB, fold(code, &i, '+'), 0}
		case '-':
			newInstruction = Instruction{ADD_SUB, -fold(code, &i, '-'), 0}
		case '>':
			newInstruction = Instruction{PTR_MOV, fold(code, &i, '>'), 0}
		case '<':
			newInstruction = Instruction{PTR_MOV, -fold(code, &i, '<'), 0}
		case '[':
			tBraceStack = append(tBraceStack, len(instructions))
			newInstruction = Instruction{JMP_ZER, 0, 0}
		case ']':
			start := tBraceStack[len(tBraceStack)-1]
			tBraceStack = tBraceStack[:len(tBraceStack)-1]
			instructions[start].Data = len(instructions)
			newInstruction = Instruction{JMP_NOT_ZER, start, 0}
		case '.':
			newInstruction = Instruction{PUT_CHR, fold(code, &i, '.'), 0}
		case ',':
			newInstruction = Instruction{RAD_CHR, 0, 0}
		case 'C':
			newInstruction = Instruction{CLR, 0, 0}
		case 'P':
			newInstruction = Instruction{MUL_CPY, copyloopMap[copyloopCounter], copyloopMulMap[copyloopCounter]}
			copyloopCounter++
		case 'R':
			newInstruction = Instruction{SCN_RGT, scanloopMap[scanloopCounter], 0}
			scanloopCounter++
		case 'L':
			newInstruction = Instruction{SCN_LFT, scanloopMap[scanloopCounter], 0}
			scanloopCounter++
		}
		instructions = append(instructions, newInstruction)
	}

	// TODO: Good ass error messages
	/*if len(tBraceStack) != 0 {
		fmt.Println("ERROR: No closing bracket")
	}*/

	return &instructions
}

func execute(cells *[]byte, cellptr *int, instructions *[]Instruction) {
	if trackStatistics {
		defer printStatistics()
	}
	defer elapsed(1)()

	instructionCount = 0
	optInstructionCount = 0

	for i, length := 0, len(*instructions); i < length; i++ {
		var currentCell = &(*cells)[*cellptr]
		var currentInstruction = (*instructions)[i]
		switch currentInstruction.Type {
		case ADD_SUB:
			*currentCell = byte(int(*currentCell) + currentInstruction.Data)
		case PTR_MOV:
			*cellptr += currentInstruction.Data
		case JMP_ZER:
			if *currentCell == 0 {
				i = currentInstruction.Data
			}
		case JMP_NOT_ZER:
			if *currentCell != 0 {
				i = currentInstruction.Data
			}
		case PUT_CHR:
			fmt.Print(strings.Repeat(string(*currentCell), currentInstruction.Data))
		case RAD_CHR:
			var b = make([]byte, 1)
			os.Stdin.Read(b)
			*currentCell = b[0]
		case CLR:
			optInstructionCount++
			*currentCell = 0
		case MUL_CPY:
			optInstructionCount++
			if *currentCell != 0 {
				(*cells)[*cellptr+currentInstruction.Data] = byte(int((*cells)[*cellptr+currentInstruction.Data]) + int(*currentCell)*currentInstruction.AuxData)
				*currentCell = 0
			}
		case SCN_RGT:
			optInstructionCount++
			if *currentCell != 0 {
				for ; *cellptr < memorySize && (*cells)[*cellptr] != 0; *cellptr += currentInstruction.Data {
				}
			}
		case SCN_LFT:
			optInstructionCount++
			if *currentCell != 0 {
				for ; *cellptr > 0 && (*cells)[*cellptr] != 0; *cellptr -= currentInstruction.Data {
				}
			}
		}
		instructionCount++
	}
}

func printStatistics() {
	var interpreterTimeString = strings.ReplaceAll(interpreterTime.String(), "0s", "<1ns")
	var preprocessorTimeString = strings.ReplaceAll(preprocessorTime.String(), "0s", "<1ns")
	var totalTimeString = strings.ReplaceAll((preprocessorTime + interpreterTime).String(), "0s", "<1ns")

	fmt.Printf("\nInstructions executed: %d (optimized: %d, optimized plaintext length: %d)\n", instructionCount, optInstructionCount, stringLength)
	fmt.Printf("Execution time: %s (VM: %s, compiler: %s)\n", totalTimeString, interpreterTimeString, preprocessorTimeString)
}

func main() {
	flag.StringVar(&filename, "i", "", "Brainfuck file to execute")
	flag.IntVar(&memorySize, "m", 30_000, "Set tape size")
	flag.BoolVar(&trackStatistics, "s", false, "Track time taken and instruction count")

	flag.Parse()

	var cellptr = 0
	var cells = make([]byte, memorySize)

	if filename != "" {
		var data, err = os.ReadFile(filename)
		if err == nil {
			var code = string(data)
			execute(&cells, &cellptr, compile(code))
		} else {
			fmt.Println("ERROR:", err)
		}
	} else {
		fmt.Println(`   _____  ____   ____  ______ `)
		fmt.Println(`  / ____|/ __ \ / __ \|  ____|`)
		fmt.Println(` | |  __| |  | | |  | | |__   `)
		fmt.Println(` | | |_ | |  | | |  | |  __|  `)
		fmt.Println(` | |__| | |__| | |__| | |     `)
		fmt.Println(`  \_____|\____/ \____/|_|     `)
		fmt.Println("")
		fmt.Println("Goof - an optimizing bf VM written in Go")
		fmt.Println("Version 1.0")
		fmt.Println("Collect statistics: ", trackStatistics)
		fmt.Println("Memory cells available: ", memorySize)
		for true {
			fmt.Print(">>> ")
			var in = bufio.NewReader(os.Stdin)
			var repl, _ = in.ReadString('\n')
			execute(&cells, &cellptr, compile(repl))
		}
	}
}
