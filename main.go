package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/mitchellh/colorstring"
)

// Instruction types
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

// Message types
const (
	Info byte = iota
	Warning
	Error
)

type Instruction struct {
	Type    byte
	Data    int
	AuxData int
}

var filename string
var memorySize int
var trackStatistics bool
var dumpMemory bool
var optPasses int

var instructionCount int
var optInstructionCount int
var stringLength int

var preprocessorTime time.Duration
var interpreterTime time.Duration
var ioWait time.Duration

func elapsed(what int) func() {
	var start = time.Now()
	return func() {
		switch what {
		case 0:
			preprocessorTime = time.Since(start)
		case 1:
			interpreterTime = time.Since(start) - ioWait
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

func parseMessage(code string, charRange [2]int, message string, msgType byte) {
	fmt.Printf("Char %d:%d ; ", charRange[0], charRange[1])
	switch msgType {
	case Info:
		colorstring.Print("[blue]INFO:[default] ")
	case Warning:
		colorstring.Print("[yellow]WARNING:[default] ")
	case Error:
		colorstring.Print("[red]ERROR:[default] ")
	}
	fmt.Println(message)
}

func dumpMem(cells *[]byte, cellptr *int) {
	var lastNonEmpty = 0
	for x := len(*cells) - 1; x > 0; x-- {
		if (*cells)[x] != 0 {
			lastNonEmpty = x
			break
		}
	}
	fmt.Println("         000 001 002 003 004 005 006 007 008 009")
	var row = 0
	for x := 0; x <= lastNonEmpty; x++ {
		if x%10 == 0 {
			if row != 0 {
				fmt.Print("\n")
			}
			fmt.Print(row, strings.Repeat(" ", 9-len(fmt.Sprint(row))))
			row = row + 10
		}
		if x == *cellptr {
			colorstring.Printf("[green]%d[default]%s", (*cells)[x], strings.Repeat(" ", 4-len(fmt.Sprint((*cells)[x]))))
		} else {
			fmt.Print((*cells)[x], strings.Repeat(" ", 4-len(fmt.Sprint((*cells)[x]))))
		}
	}
	fmt.Println("")
}

func compile(code string) (*[]Instruction, bool) {
	defer elapsed(0)()
	//* Optimize
	// Remove useless characters
	var dummyChars = regexp.MustCompile(`[^\+\-\>\<\.\,\]\[]`)
	code = dummyChars.ReplaceAllString(code, "")

	// Remove NOPs
	var nopAddSub = regexp.MustCompile(`[+-]{2,}`)
	var nopRgtLft = regexp.MustCompile(`[><]{2,}`)
	code = nopAddSub.ReplaceAllStringFunc(code, func(s string) string { return processBalanced(s, "+", "-") })
	code = nopRgtLft.ReplaceAllStringFunc(code, func(s string) string { return processBalanced(s, ">", "<") })

	var copyloopCounter int
	var copyloopMap = make([]int, 0)
	var copyloopMulMap = make([]int, 0)

	var scanloopCounter int
	var scanloopMap = make([]int, 0)

	for z := 0; z < optPasses; z++ {
		// Clearloop optimization
		var clearloop = regexp.MustCompile(`[C+-]*(?:\[[+-]+\])+\.*`) // Also delete any modifications to cell that is being cleared
		code = clearloop.ReplaceAllString(code, "C")

		// Scanloop optimization
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

		// Multiloops/copyloops optimization
		var copyloop = regexp.MustCompile(`\[-(?:[<>]+\++)+[<>]+\]|\[(?:[<>]+\++)+[<>]+-\]`)
		code = copyloop.ReplaceAllStringFunc(code, func(s string) string {
			var numOfCopies int = 0
			var offset int = 0
			if strings.Count(s, ">")-strings.Count(s, "<") == 0 {
				var tempRegex = regexp.MustCompile(`[<>]+\++`)
				for _, v := range tempRegex.FindAllString(s, -1) {
					offset += -strings.Count(v, "<") + strings.Count(v, ">")
					copyloopMap = append(copyloopMap, offset)
					copyloopMulMap = append(copyloopMulMap, strings.Count(v, "+"))
					numOfCopies++
				}
				s1 := fmt.Sprintf("%sC", strings.Repeat("P", numOfCopies))
				return s1
			} else {
				return s
			}
		})
	}

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
			if len(tBraceStack) == 0 {
				parseMessage(code, [2]int{i, i}, "Extra loop close bracket", Error)
				return nil, true
			}
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

	// *WIP*: Good error messages
	if len(tBraceStack) != 0 {
		for x := 0; x < len(tBraceStack); x++ {
			parseMessage(code, [2]int{tBraceStack[x], tBraceStack[x]}, "Missing loop close bracket", Error)
		}
		return nil, true
	}

	return &instructions, false
}

func execute(cells *[]byte, cellptr *int, code string) {
	var instructions, err = compile(code)
	if err {
		return
	}

	if trackStatistics {
		defer printStatistics()
	}

	defer elapsed(1)()

	instructionCount = 0
	optInstructionCount = 0
	var waitTime time.Time
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
			// TODO: Fix this mess
			var b = make([]byte, 1)
			waitTime = time.Now()
			os.Stdin.Read(b)
			ioWait = ioWait + time.Since(waitTime)
			*currentCell = b[0]
		case CLR:
			optInstructionCount++
			*currentCell = 0
		case MUL_CPY:
			optInstructionCount++
			if *currentCell != 0 {
				(*cells)[*cellptr+currentInstruction.Data] = byte(int((*cells)[*cellptr+currentInstruction.Data]) + int(*currentCell)*currentInstruction.AuxData)
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
	var ioTimeString = strings.ReplaceAll(ioWait.String(), "0s", "<1ns")
	var totalTimeString = strings.ReplaceAll((preprocessorTime + interpreterTime + ioWait).String(), "0s", "<1ns")

	fmt.Printf("\nInstructions executed: %d (optimized: %d, optimized plaintext length: %d)\n", instructionCount, optInstructionCount, stringLength)
	fmt.Printf("Execution time: %s (VM: %s, compiler: %s) (IO wait: %s)\n", totalTimeString, interpreterTimeString, preprocessorTimeString, ioTimeString)
}

func main() {
	flag.StringVar(&filename, "i", "", "Brainfuck file to execute")
	flag.IntVar(&memorySize, "m", 30_000, "Set tape size")
	flag.IntVar(&optPasses, "o", 2, "Number of optimization passes")
	flag.BoolVar(&trackStatistics, "s", false, "Track time taken and instruction count")
	flag.BoolVar(&dumpMemory, "dm", false, "Dump memory after execution (doesn't do anything when starting to REPL mode)")

	flag.Parse()

	var cellptr = 0
	var cells = make([]byte, memorySize)

	if filename != "" {
		var data, err = os.ReadFile(filename)
		if err == nil {
			var code = string(data)
			execute(&cells, &cellptr, code)
			fmt.Println("--------------------------------------------------------------------")
			if dumpMemory {
				dumpMem(&cells, &cellptr)
			}
		} else {
			colorstring.Println("[red]ERROR:[default] " + err.Error())
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
		fmt.Println("Version 1.0.2 (REPL mode)")
		fmt.Println("Collect statistics: ", trackStatistics)
		fmt.Println("Memory cells available: ", memorySize)
		colorstring.Println("Type [blue]help[default] to see available commands.")
		if memorySize <= 64 { // Probably useless but whatever
			colorstring.Println("[yellow]WARNING:[default] Memory might be too small!")
		}

		for true {
			fmt.Print(">>> ")
			var repl, _ = bufio.NewReader(os.Stdin).ReadString('\n')

			if strings.HasPrefix(repl, "help") {
				// TODO: Add more commands
				fmt.Println("List of available commands:")
				colorstring.Println("[blue]help[default] - print this")
				colorstring.Println("[blue]clear[default] - clear memory cells")
				colorstring.Println("[blue]viewmem[default] - displays values of memory cells, cell highlighted in [green]green[default] is the cell currently pointed to")
			} else if strings.HasPrefix(repl, "clear") {
				cellptr = 0
				cells = make([]byte, memorySize)
			} else if strings.HasPrefix(repl, "viewmem") {
				dumpMem(&cells, &cellptr)
			} else {
				execute(&cells, &cellptr, repl)
			}
		}
	}
}
