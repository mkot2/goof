package main

import (
	"bufio"
	"flag"
	"fmt"
	"math"
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

type Instruction struct {
	Type    byte
	Data    int
	AuxData int
	Offset  int
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

func fold(code *string, i *int, char byte) int {
	var count = 1
	for *i < stringLength-1 && (*code)[*i+1] == char {
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
	for x := 0; x <= int(math.Max(float64(lastNonEmpty), float64(*cellptr))); x++ {
		if x%10 == 0 {
			if row != 0 {
				fmt.Println()
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
	fmt.Println()
}

func compile(code *string) ([]Instruction, bool) {
	defer elapsed(0)()
	//* Optimize
	// Remove useless characters
	var dummyChars = regexp.MustCompile(`[^\+\-\>\<\.\,\]\[]`)
	*code = dummyChars.ReplaceAllString(*code, "")

	// Remove NOPs
	var nopAddSub = regexp.MustCompile(`[+-]{2,}`)
	var nopRgtLft = regexp.MustCompile(`[><]{2,}`)
	*code = nopAddSub.ReplaceAllStringFunc(*code, func(s string) string { return processBalanced(s, "+", "-") })
	*code = nopRgtLft.ReplaceAllStringFunc(*code, func(s string) string { return processBalanced(s, ">", "<") })

	var copyloopCounter int
	var copyloopMap = make([]int, 0)
	var copyloopMulMap = make([]int, 0)

	var scanloopCounter int
	var scanloopMap = make([]int, 0)

	for z := 0; z < optPasses; z++ {
		// Clearloop optimization
		var clearloop = regexp.MustCompile(`[C+-]*(?:\[[+-]+\])+\.*`) // Also delete any modifications to cell that is being cleared
		*code = clearloop.ReplaceAllString(*code, "C")

		// Scanloop optimization
		// TODO: Make it work with offset ops
		/*var scanloopRight = regexp.MustCompile(`\[>+\]`)
		var scanloopLeft = regexp.MustCompile(`\[<+\]`)
		*code = scanloopRight.ReplaceAllStringFunc(*code, func(s string) string {
			scanloopMap = append(scanloopMap, strings.Count(s, ">"))
			return "R"
		})
		*code = scanloopLeft.ReplaceAllStringFunc(*code, func(s string) string {
			scanloopMap = append(scanloopMap, strings.Count(s, "<"))
			return "L"
		})*/

		// Don't clear or print if cell is known zero
		var noClearPrint = regexp.MustCompile(`[RL]+C|[CRL]+\.+`)
		*code = noClearPrint.ReplaceAllString(*code, "")

		// Don't update cells if they are immediately overwritten by stdin
		var overwrite = regexp.MustCompile(`[+-C]+,`)
		*code = overwrite.ReplaceAllString(*code, ",")

		var nopLoop = regexp.MustCompile(`\[+\]+`)
		*code = nopLoop.ReplaceAllString(*code, "")

		// Multiloops/copyloops optimization
		var copyloop = regexp.MustCompile(`\[-(?:[<>]+\++)+[<>]+\]|\[(?:[<>]+\++)+[<>]+-\]`)
		*code = copyloop.ReplaceAllStringFunc(*code, func(s string) string {
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
				return fmt.Sprintf("%sC", strings.Repeat("P", numOfCopies))
			} else {
				return s
			}
		})
	}

	// Offset ops
	// Complicated but works (?)

	*code = "A" + *code

	stringLength = len(*code)
	var offsetCalc = ""
	var offsets = [][]int{}
	var offsetRow = 0
	var offsetIndex = 0
	var depth = 0

	for i := 0; i < stringLength; i++ {
		if (*code)[i] == '[' {
			if depth == 0 {
				offsetCalc += "E"
			}
			depth++
		} else if (*code)[i] == ']' {
			depth--
			if depth == 0 {
				offsetCalc += "A"
			}
		} else if depth == 0 {
			offsetCalc += string((*code)[i])
		}
	}
	fmt.Println(offsetCalc)

	var split = regexp.MustCompile(`E`).Split(offsetCalc, -1)
	for _, v := range split {
		fmt.Println(v)
		var tempSplice = []int{}
		var secondSplit = regexp.MustCompile(`(?:>+|<+|A[^$])[^<>]*`).FindAllString(v, -1)
		var lastOffset = 0
		for _, v2 := range secondSplit {
			if v2 != "A" {
				fmt.Println(v2)
				lastOffset += strings.Count(v2, ">") - strings.Count(v2, "<")
				for _, v3 := range regexp.MustCompile(`[^A<>PC]+|[PC]`).FindAllString(v2, -1) {
					for j := 0; j < len(v3)-1; j++ {
						if v3[j+1] != v3[j] {
							tempSplice = append(tempSplice, lastOffset)
						}
					}
				}
				for i := 0; i < len(regexp.MustCompile(`[^A<>PC]+|[PC]`).FindAllString(v2, -1)); i++ {
					tempSplice = append(tempSplice, lastOffset)
				}
			}
		}
		if len(tempSplice) == 0 {
			tempSplice = append(tempSplice, lastOffset)
		}

		offsets = append(offsets, tempSplice)
	}

	fmt.Println(offsets)
	// Link loops & compile
	var instructions = make([]Instruction, 0)
	var tBraceStack = make([]int, 0)
	for i := 0; i < stringLength; i++ {
		var newInstruction Instruction
		if offsetRow < len(offsets)-1 && offsetIndex == len(offsets[offsetRow])-1 && i != stringLength-1 {
			if offsetIndex != 0 {
				newInstruction = Instruction{PTR_MOV, offsets[offsetRow][offsetIndex], 0, 0}
			}
			offsetRow++
			offsetIndex = 0
			i--
		} else {
			switch (*code)[i] {
			case '+':
				if len(tBraceStack) == 0 {
					newInstruction = Instruction{ADD_SUB, fold(code, &i, '+'), 0, offsets[offsetRow][offsetIndex]}
					offsetIndex++
				} else {
					newInstruction = Instruction{ADD_SUB, fold(code, &i, '+'), 0, 0}
				}
			case '-':
				if len(tBraceStack) == 0 {
					newInstruction = Instruction{ADD_SUB, -fold(code, &i, '-'), 0, offsets[offsetRow][offsetIndex]}
					offsetIndex++
				} else {
					newInstruction = Instruction{ADD_SUB, -fold(code, &i, '-'), 0, 0}
				}
			case '>':
				if len(tBraceStack) == 0 {
					continue
				}
				newInstruction = Instruction{PTR_MOV, fold(code, &i, '>'), 0, 0}
			case '<':
				if len(tBraceStack) == 0 {
					continue
				}
				newInstruction = Instruction{PTR_MOV, -fold(code, &i, '<'), 0, 0}
			case '[':
				tBraceStack = append(tBraceStack, len(instructions))
				newInstruction = Instruction{JMP_ZER, 0, 0, 0}
			case ']':
				if len(tBraceStack) == 0 {
					//parseMessage(*code, "Extra loop close bracket", Error)
					return nil, true
				}
				start := tBraceStack[len(tBraceStack)-1]
				tBraceStack = tBraceStack[:len(tBraceStack)-1]
				instructions[start].Data = len(instructions)
				newInstruction = Instruction{JMP_NOT_ZER, start, 0, 0}
			case '.':
				if len(tBraceStack) == 0 {
					newInstruction = Instruction{PUT_CHR, fold(code, &i, '.'), 0, offsets[offsetRow][offsetIndex]}
					offsetIndex++
				} else {
					newInstruction = Instruction{PUT_CHR, fold(code, &i, '.'), 0, 0}
				}
			case ',':
				if len(tBraceStack) == 0 {
					newInstruction = Instruction{RAD_CHR, 0, 0, offsets[offsetRow][offsetIndex]}
					offsetIndex++
				} else {
					newInstruction = Instruction{RAD_CHR, 0, 0, 0}
				}
			case 'C':
				if len(tBraceStack) == 0 {
					newInstruction = Instruction{CLR, 0, 0, offsets[offsetRow][offsetIndex]}
					offsetIndex++
				} else {
					newInstruction = Instruction{CLR, 0, 0, 0}
				}
			case 'P':
				if len(tBraceStack) == 0 {
					newInstruction = Instruction{MUL_CPY, copyloopMap[copyloopCounter], copyloopMulMap[copyloopCounter], offsets[offsetRow][offsetIndex]}
					offsetIndex++
				} else {
					newInstruction = Instruction{MUL_CPY, copyloopMap[copyloopCounter], copyloopMulMap[copyloopCounter], 0}
				}
				copyloopCounter++
			case 'R':
				newInstruction = Instruction{SCN_RGT, scanloopMap[scanloopCounter], 0, 0}
				scanloopCounter++
			case 'L':
				newInstruction = Instruction{SCN_LFT, scanloopMap[scanloopCounter], 0, 0}
				scanloopCounter++
			}
		}
		instructions = append(instructions, newInstruction)
	}

	if len(tBraceStack) != 0 {
		for x := 0; x < len(tBraceStack); x++ {
			//parseMessage(*code, "Missing loop close bracket", Error)
		}
		//return nil, true
	}

	return instructions, false
}

func execute(cells *[]byte, cellptr *int, code *string) {
	var instructions, err = compile(code)
	if err {
		return
	}

	if trackStatistics {
		defer printStatistics()
	}

	defer elapsed(1)()

	// Copy cells and the cell pointer to local variables
	var cellptrL = *cellptr
	var cellsL = make([]byte, len(*cells))
	copy(cellsL, *cells)

	instructionCount = 0
	optInstructionCount = 0
	var instructionLength = len(instructions)
	for i := 0; i < instructionLength; i++ {
		var currentInstruction = instructions[i]

		switch currentInstruction.Type {
		case ADD_SUB:
			cellsL[cellptrL+currentInstruction.Offset] = byte(int(cellsL[cellptrL+currentInstruction.Offset]) + currentInstruction.Data)
		case PTR_MOV:
			cellptrL += currentInstruction.Data
		case JMP_ZER:
			if cellsL[cellptrL] == 0 {
				i = currentInstruction.Data
			}
		case JMP_NOT_ZER:
			if cellsL[cellptrL] != 0 {
				i = currentInstruction.Data
			}
		case PUT_CHR:
			fmt.Print(strings.Repeat(string(cellsL[cellptrL+currentInstruction.Offset]), currentInstruction.Data))
		case RAD_CHR:
			// TODO: Fix this
			var b = make([]byte, 1)
			var waitTime = time.Now()
			os.Stdin.Read(b)
			ioWait = ioWait + time.Since(waitTime)
			cellsL[cellptrL+currentInstruction.Offset] = b[0]
		case CLR:
			optInstructionCount++
			cellsL[cellptrL+currentInstruction.Offset] = 0
		case MUL_CPY:
			optInstructionCount++
			if cellsL[cellptrL+currentInstruction.Offset] != 0 {
				cellsL[cellptrL+currentInstruction.Data+currentInstruction.Offset] = byte(int(cellsL[cellptrL+currentInstruction.Data+currentInstruction.Offset]) + int(cellsL[cellptrL+currentInstruction.Offset])*currentInstruction.AuxData)
			}
		case SCN_RGT:
			optInstructionCount++
			for ; cellptrL < memorySize && cellsL[cellptrL] != 0; cellptrL += currentInstruction.Data {
			}
		case SCN_LFT:
			optInstructionCount++
			for ; cellptrL > 0 && cellsL[cellptrL] != 0; cellptrL -= currentInstruction.Data {
			}
		}
		instructionCount++
	}
	copy(*cells, cellsL)
	*cellptr = cellptrL
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
			execute(&cells, &cellptr, &code)
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
		fmt.Println()
		fmt.Println("Goof - an optimizing bf VM written in Go")
		fmt.Println("Version 1.0.3 (REPL mode)")
		fmt.Println("Collect statistics: ", trackStatistics)
		fmt.Println("Memory cells available: ", memorySize)
		colorstring.Println("Type [blue]help[default] to see available commands.")

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
				execute(&cells, &cellptr, &repl)
			}
		}
	}
}
