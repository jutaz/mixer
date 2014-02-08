package mysql

import (
	"encoding/binary"
	"fmt"
	"math"
)

type Stmt struct {
	conn    *conn
	id      uint32
	query   string
	columns uint16
	params  uint16
}

func (s *Stmt) Exec(args ...interface{}) (*OKPacket, error) {
	if err := s.write(args...); err != nil {
		return nil, err
	}

	return s.conn.ReadOK()
}

func (s *Stmt) Query(args ...interface{}) (*Resultset, error) {
	r, err := s.RawQuery(args...)
	if err != nil {
		return nil, err
	}

	return r.Parse(true)
}

func (s *Stmt) RawQuery(args ...interface{}) (*ResultsetPacket, error) {
	if err := s.write(args...); err != nil {
		return nil, err
	}

	return s.conn.readResultset()
}

func (s *Stmt) Close() error {
	delete(s.conn.stmts, s.query)

	if err := s.conn.WriteCommandUint32(COM_STMT_CLOSE, s.id); err != nil {
		return err
	}

	return nil
}

func (s *Stmt) write(args ...interface{}) error {
	if len(args) != int(s.params) {
		return fmt.Errorf("argument mismatch, need %d but got %d", s.params, len(args))
	}

	paramTypes := make([]byte, s.params<<1)
	paramValues := make([][]byte, s.params)

	//NULL-bitmap, length: (num-params+7)
	nullBitmap := make([]byte, (s.params+7)>>3)

	var length int = int(1 + 4 + 1 + 4 + ((s.params + 7) >> 3) + 1 + (s.params << 1))

	for i := range args {
		if args[i] == nil {
			nullBitmap[i/8] |= (1 << (uint(i) % 8))
			paramTypes[i<<1] = MYSQL_TYPE_NULL
			continue
		}

		switch v := args[i].(type) {
		case int8:
			paramTypes[i<<1] = MYSQL_TYPE_TINY
			paramValues[i] = []byte{byte(v)}
		case int16:
			paramTypes[i<<1] = MYSQL_TYPE_SHORT
			paramValues[i] = Uint16ToBytes(uint16(v))
		case int32:
			paramTypes[i<<1] = MYSQL_TYPE_LONG
			paramValues[i] = Uint32ToBytes(uint32(v))
		case int:
			paramTypes[i<<1] = MYSQL_TYPE_LONGLONG
			paramValues[i] = Uint64ToBytes(uint64(v))
		case int64:
			paramTypes[i<<1] = MYSQL_TYPE_LONGLONG
			paramValues[i] = Uint64ToBytes(uint64(v))
		case uint8:
			paramTypes[i<<1] = MYSQL_TYPE_TINY
			paramTypes[(i<<1)+1] = 0x80
			paramValues[i] = []byte{v}
		case uint16:
			paramTypes[i<<1] = MYSQL_TYPE_SHORT
			paramTypes[(i<<1)+1] = 0x80
			paramValues[i] = Uint16ToBytes(uint16(v))
		case uint32:
			paramTypes[i<<1] = MYSQL_TYPE_LONG
			paramTypes[(i<<1)+1] = 0x80
			paramValues[i] = Uint32ToBytes(uint32(v))
		case uint:
			paramTypes[i<<1] = MYSQL_TYPE_LONGLONG
			paramTypes[(i<<1)+1] = 0x80
			paramValues[i] = Uint64ToBytes(uint64(v))
		case uint64:
			paramTypes[i<<1] = MYSQL_TYPE_LONGLONG
			paramTypes[(i<<1)+1] = 0x80
			paramValues[i] = Uint64ToBytes(uint64(v))
		case bool:
			paramTypes[i<<1] = MYSQL_TYPE_TINY
			if v {
				paramValues[i] = []byte{1}
			} else {
				paramValues[i] = []byte{0}

			}
		case float32:
			paramTypes[i<<1] = MYSQL_TYPE_FLOAT
			paramValues[i] = Uint32ToBytes(math.Float32bits(v))
		case float64:
			paramTypes[i<<1] = MYSQL_TYPE_DOUBLE
			paramValues[i] = Uint64ToBytes(math.Float64bits(v))
		case string:
			paramTypes[i<<1] = MYSQL_TYPE_STRING
			paramValues[i] = append(PutLengthEncodedInt(uint64(len(v))), v...)
		case []byte:
			paramTypes[i<<1] = MYSQL_TYPE_STRING
			paramValues[i] = append(PutLengthEncodedInt(uint64(len(v))), v...)
		default:
			return fmt.Errorf("invalid argument type %T", args[i])
		}

		length += len(paramValues[i])
	}

	data := make([]byte, 4, 4+length)

	data = append(data, COM_STMT_EXECUTE)
	data = append(data, byte(s.id), byte(s.id>>8), byte(s.id>>16), byte(s.id>>24))

	//flag: CURSOR_TYPE_NO_CURSOR
	data = append(data, 0x00)

	//iteration-count, always 1
	data = append(data, 1, 0, 0, 0)

	if s.params > 0 {
		data = append(data, nullBitmap...)

		//new-params-bound-flag: 1
		data = append(data, 1)

		//type of each parameter, length: num-params * 2
		data = append(data, paramTypes...)

		//value of each parameter
		for _, v := range paramValues {
			data = append(data, v...)
		}
	}

	s.conn.Sequence = 0

	return s.conn.WritePacket(data)
}

func (c *conn) Prepare(query string) (*Stmt, error) {
	if s, ok := c.stmts[query]; ok {
		return s, nil
	}

	if err := c.WriteCommandStr(COM_STMT_PREPARE, query); err != nil {
		return nil, err
	}

	data, err := c.ReadPacket()
	if err != nil {
		return nil, err
	}

	if data[0] == ERR_HEADER {
		return nil, LoadError(data)
	} else if data[0] != OK_HEADER {
		return nil, ErrMalformPacket
	}

	s := new(Stmt)
	s.conn = c

	pos := 1

	//for statement id
	s.id = binary.LittleEndian.Uint32(data[pos:])
	pos += 4

	//number columns
	s.columns = binary.LittleEndian.Uint16(data[pos:])
	pos += 2

	//number params
	s.params = binary.LittleEndian.Uint16(data[pos:])
	pos += 2

	//warnings
	//warnings = binary.LittleEndian.Uint16(data[pos:])

	if s.columns > 0 {
		err = s.conn.readUntilEOF()
		if err != nil {
			return nil, err
		}
	}

	if s.params > 0 {
		err = s.conn.readUntilEOF()
		if err != nil {
			return nil, err
		}
	}

	c.stmts[query] = s

	return s, nil
}