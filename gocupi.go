package main

import (
	"flag"
	"fmt"
	. "github.com/BrandonAGr/gocupi/polargraph"
	"github.com/qpliu/qrencode-go/qrencode"
	"math"
	"sort"
	"strconv"
	"strings"
)

// set flag usage variable so that entire help will be output
func init() {
	flag.Usage = PrintGenericHelp
}

// main
func main() {
	Settings.Read()

	toImageFlag := flag.Bool("toimage", false, "Output result to an image file instead of to the stepper")
	toFileFlag := flag.Bool("tofile", false, "Output steps to a text file")
	toChartFlag := flag.Bool("tochart", false, "Output a chart of the movement and velocity")
	countFlag := flag.Bool("count", false, "Outputs the time it would take to draw")
	speedSlowFactor := flag.Float64("slowfactor", 1.0, "Divide max speed by this number")
	flipXFlag := flag.Bool("flipx", false, "Flip the drawing left to right")
	flipYFlag := flag.Bool("flipy", false, "Flip the drawing top to bottom")
	flag.Parse()

	if *speedSlowFactor < 1.0 {
		panic("slowfactor must be greater than 1")
	}
	// apply slow factor to max speed
	Settings.MaxSpeed_MM_S /= *speedSlowFactor
	Settings.Acceleration_Seconds *= *speedSlowFactor
	Settings.Acceleration_MM_S2 /= *speedSlowFactor

	args := flag.Args()
	if len(args) < 1 {
		PrintGenericHelp()
		return
	}

	plotCoords := make(chan Coordinate, 1024)

	switch args[0] {

	case "help":
		if len(args) != 2 {
			PrintGenericHelp()
		} else {
			PrintCommandHelp(args[1])
		}
		return

	case "test":
		plotCoords <- Coordinate{X: 0, Y: 0}
		plotCoords <- Coordinate{X: 10, Y: 0}
		plotCoords <- Coordinate{X: 10.1, Y: 0}
		plotCoords <- Coordinate{X: 10.1, Y: 10}
		plotCoords <- Coordinate{X: 10.1, Y: 10, PenUp: true}
		plotCoords <- Coordinate{X: 20.1, Y: 10, PenUp: true}
		plotCoords <- Coordinate{X: 20.1, Y: 15}
		plotCoords <- Coordinate{X: 20.1, Y: 15, PenUp: true}
		plotCoords <- Coordinate{X: 0, Y: 0, PenUp: true}
		close(plotCoords)

	case "circle":
		params := GetArgsAsFloats("circle", args[1:], 3)
		circleSetup := SlidingCircle{
			Radius:             params[0],
			CircleDisplacement: params[1],
			NumbCircles:        int(params[2]),
		}

		fmt.Println("Generating sliding circle")
		go GenerateSlidingCircle(circleSetup, plotCoords)

	case "gcode":
		if len(args) < 3 {
			PrintCommandHelp("svg")
			panic(fmt.Sprint("Expected 2 parameters and saw ", len(args)-1))
		}

		scale, _ := strconv.ParseFloat(args[1], 64)
		if scale == 0 {
			scale = 1
		}

		fmt.Println("Generating Gcode path")
		data := ParseGcodeFile(args[2])
		go GenerateGcodePath(data, scale, plotCoords)

	case "grid":
		params := GetArgsAsFloats("grid", args[1:], 2)
		gridSetup := Grid{
			Width: params[0],
			Cells: params[1],
		}

		fmt.Println("Generating grid")
		go GenerateGrid(gridSetup, plotCoords)

	case "hilbert":
		params := GetArgsAsFloats("hilbert", args[1:], 2)
		hilbertSetup := HilbertCurve{
			Size:   params[0],
			Degree: int(params[1]),
		}

		fmt.Println("Generating hilbert curve")
		go GenerateHilbertCurve(hilbertSetup, plotCoords)

	case "imagearc":
		params := GetArgsAsFloats("imagearc", args[1:], 2)
		arcSetup := Arc{
			Size:    params[0],
			ArcDist: params[1],
		}

		fmt.Println("Generating image arc path")
		data := LoadImage(args[3])
		data = GaussianImage(data)
		go GenerateArc(arcSetup, data, plotCoords)

	case "imageraster":
		params := GetArgsAsFloats("imageraster", args[1:], 2)
		rasterSetup := Raster{
			Size:     params[0],
			PenWidth: params[1],
		}

		fmt.Println("Generating image raster path")
		data := LoadImage(args[3])
		go GenerateRaster(rasterSetup, data, plotCoords)

	case "lissa":
		params := GetArgsAsFloats("lissa", args[1:], 3)
		posFunc := func(t float64) Coordinate {
			return Coordinate{
				X: params[0] * math.Cos(params[1]*t+math.Pi/2.0),
				Y: params[0] * math.Sin(params[2]*t),
			}
		}

		fmt.Println("Generating Lissajous curve")
		go GenerateParametric(posFunc, plotCoords)

	case "line":
		params := GetArgsAsFloats("line", args[1:], 2)
		lineSetup := BouncingLine{
			Angle:         params[0],
			TotalDistance: params[1],
		}

		fmt.Println("Generating line")
		go GenerateBouncingLine(lineSetup, plotCoords)

	case "move":
		PerformMouseTracking()
		return

	case "parabolic":
		params := GetArgsAsFloats("parabolic", args[1:], 3)
		parabolicSetup := Parabolic{
			Radius:           params[0],
			PolygonEdgeCount: params[1],
			Lines:            params[2],
		}

		fmt.Println("Generating parabolic graph")
		go GenerateParabolic(parabolicSetup, plotCoords)

	case "setup":
		params := GetArgsAsFloats("setup", args[1:], 3)

		Settings.SpoolHorizontalDistance_MM = params[0]
		Settings.StartingLeftDist_MM = params[1]
		Settings.StartingRightDist_MM = params[2]

		if Settings.SpoolHorizontalDistance_MM > (Settings.StartingLeftDist_MM + Settings.StartingRightDist_MM) {
			fmt.Println("ERROR: Attempted to specify a setup where the two string distances are less than the distance between idlers")
			return
		}

		Settings.CalculateDerivedFields()

		polarSystem := PolarSystemFromSettings()
		polarPos := PolarCoordinate{LeftDist: Settings.StartingLeftDist_MM, RightDist: Settings.StartingRightDist_MM}
		pos := polarPos.ToCoord(polarSystem)

		if pos.X < Settings.DrawingSurfaceMinX_MM || pos.X > Settings.DrawingSurfaceMaxX_MM || pos.Y < Settings.DrawingSurfaceMinY_MM || pos.Y > Settings.DrawingSurfaceMaxY_MM {
			fmt.Println("ERROR: The specified settings result in a pen position that exceeds the DrawingSurfaceMin/Max as defined in gocupi_config.xml")
			fmt.Printf("Initial X,Y position of pen would have been %.3f, %.3f", pos.X, pos.Y)
			fmt.Println()
		} else {
			fmt.Printf("Initial X,Y position of pen is %.3f, %.3f", pos.X, pos.Y)
			fmt.Println()
			Settings.Write()
		}

		return

	case "spiral":
		params := GetArgsAsFloats("spiral", args[1:], 2)
		spiralSetup := Spiral{
			RadiusBegin:       params[0],
			RadiusEnd:         0.01,
			RadiusDeltaPerRev: params[1],
		}

		fmt.Println("Generating spiral")
		go GenerateSpiral(spiralSetup, plotCoords)

	case "spiro":
		params := GetArgsAsFloats("spiro", args[1:], 3)
		bigR := params[0]
		littleR := params[1]
		pen := params[2]

		posFunc := func(t float64) Coordinate {
			return Coordinate{
				X: (bigR-littleR)*math.Cos(t) + pen*math.Cos(((bigR-littleR)/littleR)*t),
				Y: (bigR-littleR)*math.Sin(t) - pen*math.Sin(((bigR-littleR)/littleR)*t),
			}
		}

		fmt.Println("Generating spiro")
		go GenerateParametric(posFunc, plotCoords)

	case "spool":
		if len(args) == 3 {

			leftSpool := strings.ToLower(args[1]) == "l"
			distance := GetArgsAsFloats("spool", args[2:], 1)[0]

			MoveSpool(leftSpool, distance)
		} else {
			InteractiveMoveSpool()
		}
		return

	case "svg":
		if len(args) < 3 {
			PrintCommandHelp("svg")
			panic(fmt.Sprint("Expected at least 2 parameters and saw ", len(args)-1))
		}

		size, _ := strconv.ParseFloat(args[1], 64)
		if size == 0 {
			size = 1
		}

		svgType := "top"
		if len(args) > 3 {
			svgType = strings.ToLower(args[3])
		}

		fmt.Println("Generating svg path")
		data := ParseSvgFile(args[2])
		switch svgType {
		case "top":
			go GenerateSvgTopPath(data, size, plotCoords)

		case "box":
			go GenerateSvgBoxPath(data, size, plotCoords)

		default:
			fmt.Println("Expected top or box as the svg type, and saw", svgType)
			return
		}

	case "text":
		if len(args) != 3 {
			PrintCommandHelp("text")
			panic(fmt.Sprint("Expected 2 parameters and saw ", len(args)-1))
		}
		height, _ := strconv.ParseFloat(args[1], 64)
		if height == 0 {
			height = 40
		}

		fmt.Println("Generating text path")
		go GenerateTextPath(args[2], height, plotCoords)

	case "qr":
		params := GetArgsAsFloats("qr", args[1:], 2)
		rasterSetup := Raster{
			Size:     params[0],
			PenWidth: params[1],
		}

		fmt.Println("Generating qr raster path for ", args[3])
		data, err := qrencode.Encode(args[3], qrencode.ECLevelQ)
		if err != nil {
			panic(err)
		}
		imageData := data.ImageWithMargin(1, 0)
		go GenerateRaster(rasterSetup, imageData, plotCoords)

	default:
		PrintGenericHelp()
		return
	}

	if *flipXFlag || *flipYFlag {
		originalPlotCoords := plotCoords
		plotCoords = make(chan Coordinate, 1024)
		go FlipPlotCoords(*flipXFlag, *flipYFlag, originalPlotCoords, plotCoords)
	}

	if *toImageFlag {
		fmt.Println("Outputting to image")
		DrawToImage("output.png", plotCoords)
		return
	}

	// output the max speed and acceleration
	fmt.Println()
	fmt.Printf("MaxSpeed: %.3f mm/s Accel: %.3f mm/s^2", Settings.MaxSpeed_MM_S, Settings.Acceleration_MM_S2)
	fmt.Println()

	stepData := make(chan int8, 1024)
	go GenerateSteps(plotCoords, stepData)
	switch {
	case *countFlag:
		CountSteps(stepData)
	case *toFileFlag:
		WriteStepsToFile(stepData)
	case *toChartFlag:
		WriteStepsToChart(stepData)
	default:
		WriteStepsToSerial(stepData)
	}
}

func FlipPlotCoords(flipX, flipY bool, coords <-chan Coordinate, flippedCoords chan<- Coordinate) {
	defer close(flippedCoords)
	for coord := range coords {
		if flipX {
			coord.X = -coord.X
		}
		if flipY {
			coord.Y = -coord.Y
		}
		flippedCoords <- coord
	}
}

// Parse a series of numbers as floats
func GetArgsAsFloats(command string, args []string, expectedCount int) []float64 {

	if len(args) < expectedCount {
		PrintCommandHelp(command)
		panic(fmt.Sprint("Expected at least ", expectedCount, " numeric parameters and only saw ", len(args)))
	}

	numbers := make([]float64, expectedCount)

	var err error
	for argIndex := 0; argIndex < expectedCount; argIndex++ {
		if numbers[argIndex], err = strconv.ParseFloat(args[argIndex], 64); err != nil {
			panic(fmt.Sprint("Unable to parse", args[argIndex], "as a float: ", err))
		}

		if numbers[argIndex] == 0 {
			panic(fmt.Sprint("0 is not a valid value for parameter", argIndex))
		}
	}

	return numbers
}

// output the help for a specific command
func PrintCommandHelp(command string) {

	helpText, ok := CommandHelp[command]
	if !ok {
		fmt.Println("Unrecognized command: " + command)
		PrintGenericHelp()
	}
	fmt.Println(helpText)
}

// output help summary
func PrintGenericHelp() {
	fmt.Println(`help COMMAND to view help for a specific command
	
General Usage: (flags) COMMAND PARAMETERS...

All distance numbers are in millimeters
All angles are in radians

Flags:
-toimage, outputs data to an image of what the render should look like
-tochart, outputs a graph of velocity and position
-tofile, outputs step data to a file
-count, outputs number of steps and render time
-slowfactor=#, slow down rendering by #x, 2x, 4x slower etc
-flipx, flip the generated image left to right
-flipx, flip the generated image top to bottom

Commands:`)

	// output list of possible commands
	var keys []string
	for k := range CommandHelp {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	first := true
	for _, k := range keys {
		if !first {
			fmt.Print(", ")
		} else {
			first = false
		}
		fmt.Print(k)
	}
}

var CommandHelp = map[string]string{
	`circle`: `Draw a number of corkscrew kind of sliding circle pattern.
	
circle R d n
	R - radius of circle
	d - displacement per revolution
	n - number of circles`,

	`gcode`: `Render a given gcode file, only a subset of valid gcode is recognized.
	
gcode s "path"
	s - scale
	path - path to the gcode file`,

	`grid`: `Draw a grid, starting in the upper left.
	
grid s c
	s - size of square grid
	c - number of cells in grid`,

	`hilbert`: `Draw a hilbert space filling curve.
	
hilbert s d
	s - size of square
	d - degree of hilbert curve, 2 to 6`,

	`imagearc`: `Draw an image using an arc pattern and drawing a thicker line to represent darker parts of the image.
	
imagearc s a "path"
	s - size of long axis
	a - distance between each arc`,

	`imageraster`: `Draw an image using horizontal line pattern and drawing thicker lines to represent darker parts of the image.
	
imageraster s p "path"
	s - size of long axis
	p - pen thickness / distance between rows`,

	`lissa`: `Draw a lissajous curve, drawing stops when the pen arrives back at the starting position.
	
lissa s a b
	s - size of drawing
	a - first factor
	b - second factor`,

	`line`: `Draw a straight line at the given angle and distance from the current position.
	
line a d
	a - initial angle to start drawing
	d - distance in meters for line`,

	`move`: `Enter a mouse based interactive movement mode, allows you to position the pen to start a new drawing or to manually move the pen to a known calibration position.`,

	`parabolic`: `Draw a series of parabolic curves (curves made out of a series of straight lines).
	
parabolic R c l
	R - radius of shape
	c - count of polygon edges
	l - number of lines per edges`,

	`setup`: `Enter the initial setup measurements of the system.
	
setup D L R
	D - distance between the idlers
	L - length of left string from left idler to pen tip
	R - length of right string from right idler to pen tip`,

	`spiral`: `Draw a spiral.
	
spiral R d
	R - initial outter radius
	d - radius delta per revolution`,

	`spiro`: `Draw a spirograph type image, drawing stops when the pen arrives back at the starting position.
	
spiro R r p
	R - first circle radius
	r - second circle radius
	p - pen distance`,

	`spool`: `Directly control spool movement, useful for initial setup. If you ommit the L/R d parameters then you enter an interactive mode where you can repeatedly type the options to enter several spool commands in a row.
	
spool [L|R] d
	L|R - designing either the left or right spool
	d - distance to extend line, negative numbers retract`,

	`svg`: `Draw an svg file. Must be made up of only straight lines, curves are not currently supported in the svg parser.
	
svg s "path" t
	s - size of long axis
	path - path to svg file
	t - type of drawing, either top or box
		top (default) - best for TSP single loop drawings, pen starts on loop at top
		box - pen starts in upper left corner, drawing boundary extents first`,

	`text`: `Draw a given text string, font is based on the hershey simplex font.
	
text h "string"
	h - letter height
	string - text to print`,

	`qr`: `Draw a QR code.
	
qr s p "string"
	s - size of square
	p - pen thickness, determines how much it fills in solid squares
	string - the text that will be encoded`,
}