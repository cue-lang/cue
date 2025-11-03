package token

type PortablePosition struct {
	Filepath string `json:"filepath"`
	Offset   int    `json:"offset"`
}

func (p Pos) ToPortable() PortablePosition {
	if p == NoPos {
		return PortablePosition{}
	}
	return PortablePosition{
		Filepath: p.file.name,
		Offset:   p.offset,
	}
}

type PortableError struct {
	PositionJSON       PortablePosition   `json:"position"`
	InputPositionsJSON []PortablePosition `json:"input_positions"`
	ErrorJSON          string             `json:"error"`
	PathJSON           []string           `json:"paths"`
	MsgJSON            string             `json:"msg"`
}

func (p *PortableError) Position() Pos {
	return Pos{
		file:   NewFile(p.PositionJSON.Filepath, 0, 0),
		offset: p.PositionJSON.Offset,
	}
}

func (p *PortableError) InputPositions() []Pos {
	poss := make([]Pos, len(p.InputPositionsJSON))
	for i, pos := range p.InputPositionsJSON {
		poss[i] = Pos{
			file:   NewFile(pos.Filepath, 0, 0),
			offset: pos.Offset,
		}
	}
	return poss
}

func (p *PortableError) Error() string {
	return p.ErrorJSON
}

func (p *PortableError) Path() []string {
	return p.PathJSON
}

func (p *PortableError) Msg() (format string, args []interface{}) {
	return "%s", []interface{}{p.MsgJSON}
}
