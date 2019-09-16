package xnet

import (
	"context"
	"io"
	"log"
	"net"
	"sync"
)

type XConn interface {
	net.Conn
}

type XConnImpl struct {
	conn      net.Conn
	readChan  chan []byte
	writeChan chan []byte
	ctx       context.Context
	cancel    func()
	sync.Mutex
}

func NewXConn(conn net.Conn) *XConnImpl {
	ctx, cancel := context.WithCancel(context.Background())
	xi := &XConnImpl{conn: conn, readChan: make(chan []byte, 0), writeChan: make(chan []byte, 0), ctx: ctx, cancel: cancel}
	go func() { //读数据
		for {
			select {
			case <-ctx.Done():
				goto END
			default:
				if !xi.read() {
					xi.cancel()
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
					xi.cancel()
					log.Println("写数据错误:", err.Error())
					goto END
				}
			}
		}
	END:
		xi.conn.Close()
		close(xi.writeChan)
		log.Println("退出写数据")
	}()
	return xi
}

func (xi *XConnImpl) read() bool {
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
	body := make([]byte, 0)
	for {
		stepData := make([]byte, 1024)
		n, err = xi.conn.Read(stepData)
		if err != nil {
			if err == io.EOF {
				break
			}
			log.Println("读数据体错误:", err.Error())
			return false
		}
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

func (xi *XConnImpl) Close() {
	log.Println("服务端主动关闭")
	xi.cancel()
}

// 纯发送数据读写，不带额外信息及定义的协议
func (xi *XConnImpl) Read() ([]byte, bool) {
	select {
	case <-xi.ctx.Done():
		return nil, false
	default:
		bs := <-xi.readChan
		return bs, true
	}
}

func (xi *XConnImpl) Write(data []byte) bool {
	select {
	case <-xi.ctx.Done():
		return false
	default:
		xi.writeChan <- data
		return true
	}
}

// 同步写读数据,并发安全 TOTEST
func (xi *XConnImpl) WriteReads(in []byte) ([]byte, bool) {
	xi.Lock()
	defer xi.Unlock()
	if !xi.Write(in) {
		return nil, false
	}
	return xi.Read()
}
