/*
 * This file is subject to the terms and conditions defined in
 * file 'LICENSE.md', which is part of this source code package.
 */

package huffman

import (
	"errors"

	"github.com/bcmmbaga/unipdf-agpl/v3/internal/jbig2/reader"
)

// StandardTable is the structure that defines standard jbig2 table.
type StandardTable struct {
	rootNode *InternalNode
}

// Decode implements  Node interface.
func (s *StandardTable) Decode(r reader.StreamReader) (int64, error) {
	return s.rootNode.Decode(r)
}

// InitTree implements Tabler interface.
func (s *StandardTable) InitTree(codeTable []*Code) error {
	preprocessCodes(codeTable)

	for _, c := range codeTable {
		if err := s.rootNode.append(c); err != nil {
			return err
		}
	}

	return nil
}

// String implements Stringer interface.
func (s *StandardTable) String() string {
	return s.rootNode.String() + "\n"
}

// RootNode implements Tabler interface.
func (s *StandardTable) RootNode() *InternalNode {
	return s.rootNode
}

// GetStandardTable gets the standard table for the given 'number'.
func GetStandardTable(number int) (Tabler, error) {
	if number <= 0 || number > len(standardTables) {
		return nil, errors.New("Index out of range")
	}

	table := standardTables[number-1]

	if table == nil {
		var err error
		table, err = newStandardTable(tables[number-1])
		if err != nil {
			return nil, err
		}

		// set the table at standardTables
		standardTables[number-1] = table
	}
	return table, nil
}

func newStandardTable(table [][]int32) (*StandardTable, error) {
	var codeTable []*Code

	for i := 0; i < len(table); i++ {
		prefixLength := table[i][0]
		rangeLength := table[i][1]
		rangeLow := table[i][2]
		var isLowerRange bool

		if len(table[i]) > 3 {
			isLowerRange = true
		}

		codeTable = append(codeTable, NewCode(prefixLength, rangeLength, rangeLow, isLowerRange))
	}

	s := &StandardTable{rootNode: newInternalNode(0)}
	if err := s.InitTree(codeTable); err != nil {
		return nil, err
	}
	return s, nil
}

var tables = [][][]int32{
	// B1
	{
		{1, 4, 0},
		{2, 8, 16},
		{3, 16, 272},
		{3, 32, 65808}, //  high

	},
	// B2
	{
		{1, 0, 0},
		{2, 0, 1},
		{3, 0, 2},
		{4, 3, 3},
		{5, 6, 11},
		{6, 32, 75}, //  high
		{6, -1, 0},  //  OOB
	},
	// B3
	{
		{8, 8, -256},
		{1, 0, 0},
		{2, 0, 1},
		{3, 0, 2},
		{4, 3, 3},
		{5, 6, 11},
		{8, 32, -257, 999}, //  low
		{7, 32, 75},        //  high
		{6, -1, 0},         //  OOB
	},
	// B4
	{
		{1, 0, 1},
		{2, 0, 2},
		{3, 0, 3},
		{4, 3, 4},
		{5, 6, 12},
		{5, 32, 76}, //  high
	},
	// B5
	{
		{7, 8, -255},
		{1, 0, 1},
		{2, 0, 2},
		{3, 0, 3},
		{4, 3, 4},
		{5, 6, 12},
		{7, 32, -256, 999}, //  low
		{6, 32, 76},        //  high
	},
	// B6
	{
		{5, 10, -2048},
		{4, 9, -1024},
		{4, 8, -512},
		{4, 7, -256},
		{5, 6, -128},
		{5, 5, -64},
		{4, 5, -32},
		{2, 7, 0},
		{3, 7, 128},
		{3, 8, 256},
		{4, 9, 512},
		{4, 10, 1024},
		{6, 32, -2049, 999}, //  low
		{6, 32, 2048},       //  high
	},
	// B7
	{
		{4, 9, -1024},
		{3, 8, -512},
		{4, 7, -256},
		{5, 6, -128},
		{5, 5, -64},
		{4, 5, -32},
		{4, 5, 0},
		{5, 5, 32},
		{5, 6, 64},
		{4, 7, 128},
		{3, 8, 256},
		{3, 9, 512},
		{3, 10, 1024},
		{5, 32, -1025, 999}, //  low
		{5, 32, 2048},       //  high
	},
	// B8
	{
		{8, 3, -15},
		{9, 1, -7},
		{8, 1, -5},
		{9, 0, -3},
		{7, 0, -2},
		{4, 0, -1},
		{2, 1, 0},
		{5, 0, 2},
		{6, 0, 3},
		{3, 4, 4},
		{6, 1, 20},
		{4, 4, 22},
		{4, 5, 38},
		{5, 6, 70},
		{5, 7, 134},
		{6, 7, 262},
		{7, 8, 390},
		{6, 10, 646},
		{9, 32, -16, 999}, //  low
		{9, 32, 1670},     //  high
		{2, -1, 0},        //  OOB
	},
	// B9
	{
		{8, 4, -31},
		{9, 2, -15},
		{8, 2, -11},
		{9, 1, -7},
		{7, 1, -5},
		{4, 1, -3},
		{3, 1, -1},
		{3, 1, 1},
		{5, 1, 3},
		{6, 1, 5},
		{3, 5, 7},
		{6, 2, 39},
		{4, 5, 43},
		{4, 6, 75},
		{5, 7, 139},
		{5, 8, 267},
		{6, 8, 523},
		{7, 9, 779},
		{6, 11, 1291},
		{9, 32, -32, 999}, //  low
		{9, 32, 3339},     //  high
		{2, -1, 0},        //  OOB
	},
	// B10
	{
		{7, 4, -21},
		{8, 0, -5},
		{7, 0, -4},
		{5, 0, -3},
		{2, 2, -2},
		{5, 0, 2},
		{6, 0, 3},
		{7, 0, 4},
		{8, 0, 5},
		{2, 6, 6},
		{5, 5, 70},
		{6, 5, 102},
		{6, 6, 134},
		{6, 7, 198},
		{6, 8, 326},
		{6, 9, 582},
		{6, 10, 1094},
		{7, 11, 2118},
		{8, 32, -22, 999}, //  low
		{8, 32, 4166},     //  high
		{2, -1, 0},        //  OOB
	},
	// B11
	{
		{1, 0, 1},
		{2, 1, 2},
		{4, 0, 4},
		{4, 1, 5},
		{5, 1, 7},
		{5, 2, 9},
		{6, 2, 13},
		{7, 2, 17},
		{7, 3, 21},
		{7, 4, 29},
		{7, 5, 45},
		{7, 6, 77},
		{7, 32, 141}, //  high
	},
	// B12
	{
		{1, 0, 1},
		{2, 0, 2},
		{3, 1, 3},
		{5, 0, 5},
		{5, 1, 6},
		{6, 1, 8},
		{7, 0, 10},
		{7, 1, 11},
		{7, 2, 13},
		{7, 3, 17},
		{7, 4, 25},
		{8, 5, 41},
		{8, 32, 73},
	},
	// B13
	{
		{1, 0, 1},
		{3, 0, 2},
		{4, 0, 3},
		{5, 0, 4},
		{4, 1, 5},
		{3, 3, 7},
		{6, 1, 15},
		{6, 2, 17},
		{6, 3, 21},
		{6, 4, 29},
		{6, 5, 45},
		{7, 6, 77},
		{7, 32, 141}, //  high
	},
	// B14
	{
		{3, 0, -2},
		{3, 0, -1},
		{1, 0, 0},
		{3, 0, 1},
		{3, 0, 2},
	},
	// B15
	{
		{7, 4, -24},
		{6, 2, -8},
		{5, 1, -4},
		{4, 0, -2},
		{3, 0, -1},
		{1, 0, 0},
		{3, 0, 1},
		{4, 0, 2},
		{5, 1, 3},
		{6, 2, 5},
		{7, 4, 9},
		{7, 32, -25, 999}, //  low
		{7, 32, 25},       //  high
	},
}

var standardTables = make([]Tabler, len(tables))
