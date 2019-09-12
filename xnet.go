package xnet

import (
	"context"
	"log"
	"net"
)

type XConn interface {
	net.Conn
}

type XConnImpl struct {
	net.Conn
	readChan  chan []byte
	writeChan chan []byte
	ctx       context.Context
	cancel    func()
}

func NewXConn(conn net.Conn) *XConnImpl {
	ctx, cancel := context.WithCancel(context.Background())
	xi := &XConnImpl{Conn: conn, readChan: make(chan []byte, 0), writeChan: make(chan []byte, 0), ctx: ctx, cancel: cancel}
	go func() { //读数据
		for {
			select {
			case <-ctx.Done():
				goto END
			default:
				bs := make([]byte, 1024)
				n, err := xi.Conn.Read(bs)
				if err != nil {
					log.Println("读数据错误:", err.Error())
					goto END
				}
				xi.readChan <- bs[0:n]
			}
		}
	END:
		xi.Conn.Close()
		close(xi.readChan)
		log.Println("退出读数据")
	}()
	go func() { //写数据
		for {
			select {
			case <-ctx.Done():
				goto END
			case bs := <-xi.writeChan:
				if _, err := conn.Write(bs); err != nil {
					log.Println("写数据错误:", err.Error())
					goto END
				}
			}
		}
	END:
		xi.Conn.Close()
		close(xi.writeChan)
		log.Println("退出写数据")
	}()
	return xi
}

func (xi *XConnImpl) Read() ([]byte, bool) {
	for {
		select {
		case <-xi.ctx.Done():
			return nil, false
		default:
			bs := <-xi.readChan
			return bs, true
		}
	}
}

func (xi *XConnImpl) Write(data []byte) bool {
	for {
		select {
		case <-xi.ctx.Done():
			return false
		default:
			xi.writeChan <- data
			return true
		}
	}
}

func (xi *XConnImpl) Close() {
	xi.cancel()
}
