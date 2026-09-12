package roads

import (
	"bufio"
	"bytes"
	"regexp"
	"strconv"
	"strings"
)

// Older prefabs are TorqueScript rather than JSON, and a level can hold dozens
// of them — hirochi_raceway has 56 against 8 JSON ones, and that is where its
// bridges live. The shape is:
//
//	new SimGroup(bridge) {
//	   new DecalRoad(road_1) {
//	      Material = "road_asphalt_2lane";
//	      drivability = "1";
//	      Node = "-100 200 30 8";
//	      Node = "-90 210 30 8";
//	   };
//	};
//
// Field names vary in case between versions and objects nest, so the parser
// matches case-insensitively and tracks brace depth rather than assuming shape.
var (
	torqueObject = regexp.MustCompile(`(?i)^new\s+([A-Za-z_][A-Za-z0-9_]*)\s*\(`)
	torqueField  = regexp.MustCompile(`(?i)^([A-Za-z_][A-Za-z0-9_]*)\s*=\s*"?([^";]*)"?\s*;`)
)

type torqueFrame struct {
	depth int
	road  *Road
	nodes [][]float64
}

// looksLikeTorque distinguishes a TorqueScript prefab from a JSON one without
// parsing either.
func looksLikeTorque(data []byte) bool {
	head := data
	if len(head) > 4096 {
		head = head[:4096]
	}
	return bytes.Contains(head, []byte("new ")) && !bytes.Contains(head, []byte(`"class"`))
}

// readTorque pulls every road out of a TorqueScript prefab.
func readTorque(data []byte, into *Level) int {
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	depth := 0
	var stack []*torqueFrame
	found := 0

	flush := func(frame *torqueFrame) {
		if frame.road == nil || len(frame.nodes) < 2 {
			return
		}
		road := finishTorqueRoad(frame)
		if road == nil {
			return
		}
		addRoad(into, road)
		found++
	}

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "//") {
			continue
		}

		if match := torqueObject.FindStringSubmatch(line); match != nil {
			class := match[1]
			frame := &torqueFrame{depth: depth}
			if strings.EqualFold(class, "DecalRoad") || strings.EqualFold(class, "MeshRoad") {
				frame.road = &Road{Class: canonicalClass(class)}
			}
			stack = append(stack, frame)
		} else if len(stack) > 0 && stack[len(stack)-1].road != nil {
			readTorqueField(stack[len(stack)-1], line)
		}

		depth += strings.Count(line, "{") - strings.Count(line, "}")

		// A frame closes when the depth falls back to where it opened.
		for len(stack) > 0 && depth <= stack[len(stack)-1].depth {
			frame := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			flush(frame)
		}
	}
	return found
}

func canonicalClass(class string) string {
	if strings.EqualFold(class, "MeshRoad") {
		return "MeshRoad"
	}
	return "DecalRoad"
}

func readTorqueField(frame *torqueFrame, line string) {
	match := torqueField.FindStringSubmatch(line)
	if match == nil {
		return
	}
	key := strings.ToLower(match[1])
	value := strings.TrimSpace(match[2])
	switch key {
	case "node":
		// "x y z width [depth …]" — the first four numbers mean the same thing
		// as the first four in a JSON node.
		parts := strings.Fields(value)
		if len(parts) < 4 {
			return
		}
		node := make([]float64, 0, 4)
		for i := 0; i < 4; i++ {
			number, err := strconv.ParseFloat(parts[i], 64)
			if err != nil {
				return
			}
			node = append(node, number)
		}
		frame.nodes = append(frame.nodes, node)
	case "material":
		frame.road.Material = value
	case "drivability":
		if number, err := strconv.ParseFloat(value, 64); err == nil {
			frame.road.Drivability = number
		}
	case "internalname", "name":
		if frame.road.Group == "" {
			frame.road.Group = value
		}
	}
}

func finishTorqueRoad(frame *torqueFrame) *Road {
	road := frame.road
	if road.Group == "" {
		road.Group = "prefab"
	}
	road.Invisible = strings.Contains(strings.ToLower(road.Material), "invisible")
	road.Bounds = [4]float64{inf(1), inf(1), inf(-1), inf(-1)}
	for _, values := range frame.nodes {
		node := Node{round(values[0], 2), round(values[1], 2), round(values[2], 2), round(values[3], 2)}
		road.Nodes = append(road.Nodes, node)
		road.Bounds[0] = min(road.Bounds[0], node[0])
		road.Bounds[1] = min(road.Bounds[1], node[1])
		road.Bounds[2] = max(road.Bounds[2], node[0])
		road.Bounds[3] = max(road.Bounds[3], node[1])
	}
	if len(road.Nodes) < 2 {
		return nil
	}
	return road
}
