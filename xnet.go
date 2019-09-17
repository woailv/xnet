package xnet

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"sync"
)

type XConn interface {
	ReadStr() (string, bool)
	WriteStr(str string) bool
	WriteReadStrs(str string) (string, bool)
	WriteStrReadJsons(str string, out interface{}) bool
	Close()
}

type xconnImpl struct {
	conn      net.Conn
	readChan  chan []byte
	writeChan chan []byte
	ctx       context.Context
	cancel    func()
	sync.Mutex
	oneceFunc sync.Once
}

func NewXConn(conn net.Conn) XConn {
	ctx, cancel := context.WithCancel(context.Background())
	xi := &xconnImpl{
		conn:      conn,
		readChan:  make(chan []byte, 0),
		writeChan: make(chan []byte, 0),
		ctx:       ctx,
		cancel:    cancel,
		// oneceFunc:sync.Once{}
	}
	go func() { //读数据
		for {
			select {
			case <-ctx.Done():
				goto END
			default:
				if !xi.read() {
					xi.Close()
					goto END
				}
			}
		}
	END:
		xi.conn.Close()
		close(xi.readChan)
		log.Println("退出读数据")
	}()
	go func() { //写数据
		for {
			select {
			case <-ctx.Done():
				goto END
			case bs := <-xi.writeChan:
				if _, err := xi.conn.Write(append(int2Bytes(len(bs)), bs...)); err != nil {
					xi.Close()
					log.Println("写数据错误:", err.Error())
					goto END
				}
			}
		}
	END:
		close(xi.writeChan)
		log.Println("退出写数据")
	}()
	return xi
}

func (xi *xconnImpl) read() bool {
	bodyLenbs := make([]byte, 4)
	n, err := xi.conn.Read(bodyLenbs)
	if err != nil {
		log.Println("读数据体长度错误:", err.Error())
		return false
	}
	if n < 4 {
		log.Printf("读到数据体长度字节数:%d,不足4个字节", n)
		return false
	}
	bodyLen := bytes2Int(bodyLenbs)
	log.Println("要读取的数据体长度:", bodyLen)
	body := make([]byte, 0)
	if bodyLen <= 0 {
		xi.readChan <- body
		return true
	}
	readLen := bodyLen //剩余读取字节数
	for {
		stepLen := 1024
		if stepLen > readLen {
			stepLen = readLen
		}
		log.Println("读取字节步长:", stepLen)
		stepData := make([]byte, stepLen)
		log.Println("reading...")
		n, err = xi.conn.Read(stepData)
		if err != nil {
			if err == io.EOF {
				break
			}
			log.Println("读数据体错误:", err.Error())
			return false
		}
		readLen -= n
		log.Println("单次读取字节数:", n)
		if n == 0 {
			break
		}
		body = append(body, stepData[:n]...)
		if len(body) == bodyLen {
			break
		}
	}
	if len(body) < bodyLen {
		log.Println("读数据体字节长度不足错误:", err.Error())
		return false
	}
	xi.readChan <- body
	return true
}

func (xi *xconnImpl) Close() {
	xi.oneceFunc.Do(func() {
		log.Println("服务端close")
		xi.cancel()
		xi.conn.Close()
	})
}

// 纯发送数据读写，不带额外信息及定义的协议
func (xi *xconnImpl) Read() ([]byte, bool) {
	select {
	case <-xi.ctx.Done():
		return nil, false
	default:
		bs := <-xi.readChan
		return bs, true
	}
}

func (xi *xconnImpl) ReadStr() (string, bool) {
	bs, ok := xi.Read()
	if !ok {
		return "", false
	}
	return string(bs), true
}

func (xi *xconnImpl) Write(data []byte) bool {
	select {
	case <-xi.ctx.Done():
		return false
	default:
		xi.writeChan <- data
		return true
	}
}

func (xi *xconnImpl) WriteStr(str string) bool {
	return xi.Write([]byte(str))
}

// 同步写读数据,并发安全 TOTEST
func (xi *xconnImpl) WriteReads(in []byte) ([]byte, bool) {
	xi.Lock()
	defer xi.Unlock()
	if !xi.Write(in) {
		return nil, false
	}
	return xi.Read()
}

// 同步写字符串读json
func (xi *xconnImpl) WriteStrReadJsons(str string, out interface{}) bool {
	bs, ok := xi.WriteReads([]byte(str))
	if !ok {
		return false
	}
	if err := json.Unmarshal(bytes.Replace(bs, []byte{13, 10}, []byte{}, -1), out); err != nil { //已替换掉换行符
		fmt.Sprintf("json反序列化错误,dataString:%s,errmsg:%s", string(bs), err.Error())
		return false
	}
	return true
}

// 同步写字符串读字符串
func (xi *xconnImpl) WriteReadStrs(str string) (string, bool) {
	bs, ok := xi.WriteReads([]byte(str))
	if !ok {
		return "", false
	}
	return string(bs), true
}
