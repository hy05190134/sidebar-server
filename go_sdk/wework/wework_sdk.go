package wework

/*
#cgo CFLAGS: -I.
#cgo LDFLAGS: -L../ -lWeWorkFinanceSdk_C
#include "WeWorkFinanceSdk_C.h"
#include <stdlib.h>
*/
import "C"
import (
	"errors"
	"unsafe"
)

// SDK 错误码定义
const (
	ErrCodeInvalidParam    = 10000 // 参数错误
	ErrCodeInvalidSecret   = 10001 // 密钥错误
	ErrCodeDataDecryptFail = 10002 // 数据解密失败
	ErrCodeSystemFail      = 10003 // 系统失败
	ErrCodeKeyDecryptFail  = 10004 // 密钥解密失败
	ErrCodeFileIDError     = 10005 // fileid错误
	ErrCodeDecryptFail     = 10006 // 解密失败
	ErrCodeKeyVersionError = 10007 // 找不到信息加密版本对应的私钥，需要重新下载私钥
	ErrCodeEncryptKeyError = 10008 // 解密encrypt_key失败
	ErrCodeIPForbidden     = 10009 // ip白名单
	ErrCodeDataExpired     = 10010 // 数据过期
	ErrCodeCertError       = 10011 // 证书错误
)

// SDK 实例
type SDK struct {
	sdk *C.WeWorkFinanceSdk_t
}

// ChatData 会话存档数据
type ChatData struct {
	Data string
	Len  int
}

// MediaData 媒体数据
type MediaData struct {
	Data     []byte
	OutIndex string
	IsFinish bool
	DataLen  int
	IndexLen int
}

// NewSDK 创建新的 SDK 实例
func NewSDK() *SDK {
	return &SDK{
		sdk: C.NewSdk(),
	}
}

// Init 初始化 SDK
// corpid: 企业微信的企业id，例如：wwd08c8exxxx5ab44d
// secret: 会话内容存档的Secret
func (s *SDK) Init(corpid, secret string) error {
	if s.sdk == nil {
		return errors.New("SDK instance is nil")
	}

	corpidCStr := C.CString(corpid)
	defer C.free(unsafe.Pointer(corpidCStr))

	secretCStr := C.CString(secret)
	defer C.free(unsafe.Pointer(secretCStr))

	ret := C.Init(s.sdk, corpidCStr, secretCStr)
	if ret != 0 {
		return getError(int(ret))
	}

	return nil
}

// GetChatData 获取会话存档数据
// seq: 从指定的seq开始拉取消息，注意返回的消息从seq+1开始返回，seq为之前接口返回的最大seq值。首次使用请使用seq:0
// limit: 一次拉取的消息数量，最大值1000，超过1000会返回错误
// proxy: 使用代理访问时需要传入代理连接。如：socks5://10.0.0.1:8081 或者 http://10.0.0.1:8081
// passwd: 代理账号密码，不需要代理时账号密码为空即可。如：user_name:passwd_123
// timeout: 超时时间，单位秒
func (s *SDK) GetChatData(seq uint64, limit uint32, proxy, passwd string, timeout int) (*ChatData, error) {
	if s.sdk == nil {
		return nil, errors.New("SDK instance is nil")
	}

	proxyCStr := C.CString(proxy)
	defer C.free(unsafe.Pointer(proxyCStr))

	passwdCStr := C.CString(passwd)
	defer C.free(unsafe.Pointer(passwdCStr))

	chatDatas := C.NewSlice()
	defer C.FreeSlice(chatDatas)

	ret := C.GetChatData(s.sdk, C.ulonglong(seq), C.uint(limit), proxyCStr, passwdCStr, C.int(timeout), chatDatas)
	if ret != 0 {
		return nil, getError(int(ret))
	}

	data := C.GoStringN(chatDatas.buf, C.int(chatDatas.len))
	return &ChatData{
		Data: data,
		Len:  int(chatDatas.len),
	}, nil
}

// GetMediaData 获取媒体文件数据
// indexbuf: 媒体消息分片拉取，需要传入每次拉取的索引信息。首次不需要填写，默认拉取512k，之后每次调用只需要将上次调用返回的outindexbuf传入即可。
// sdkFileid: 从GetChatData返回的会话消息中，媒体消息包含的sdkfileid
// proxy: 使用代理访问时需要传入代理连接。如：socks5://10.0.0.1:8081 或者 http://10.0.0.1:8081
// passwd: 代理账号密码，不需要代理时账号密码为空即可。如：user_name:passwd_123
// timeout: 超时时间，单位秒
func (s *SDK) GetMediaData(indexbuf, sdkFileid, proxy, passwd string, timeout int) (*MediaData, error) {
	if s.sdk == nil {
		return nil, errors.New("SDK instance is nil")
	}

	indexbufCStr := C.CString(indexbuf)
	defer C.free(unsafe.Pointer(indexbufCStr))

	sdkFileidCStr := C.CString(sdkFileid)
	defer C.free(unsafe.Pointer(sdkFileidCStr))

	proxyCStr := C.CString(proxy)
	defer C.free(unsafe.Pointer(proxyCStr))

	passwdCStr := C.CString(passwd)
	defer C.free(unsafe.Pointer(passwdCStr))

	mediaData := C.NewMediaData()
	defer C.FreeMediaData(mediaData)

	ret := C.GetMediaData(s.sdk, indexbufCStr, sdkFileidCStr, proxyCStr, passwdCStr, C.int(timeout), mediaData)
	if ret != 0 {
		return nil, getError(int(ret))
	}

	dataPtr := C.GetData(mediaData)
	dataLen := int(C.GetDataLen(mediaData))
	data := C.GoBytes(unsafe.Pointer(dataPtr), C.int(dataLen))

	outIndexPtr := C.GetOutIndexBuf(mediaData)
	outIndexLen := int(C.GetIndexLen(mediaData))
	outIndex := C.GoStringN(outIndexPtr, C.int(outIndexLen))

	isFinish := C.IsMediaDataFinish(mediaData) != 0

	return &MediaData{
		Data:     data,
		OutIndex: outIndex,
		IsFinish: isFinish,
		DataLen:  dataLen,
		IndexLen: outIndexLen,
	}, nil
}

// DecryptData 解密会话存档数据
// encryptKey: getchatdata返回的encrypt_random_key，使用企业自己的对应版本密钥RSA解密后得到
// encryptMsg: getchatdata返回的encrypt_chat_msg
func DecryptData(encryptKey, encryptMsg string) (string, error) {
	encryptKeyCStr := C.CString(encryptKey)
	defer C.free(unsafe.Pointer(encryptKeyCStr))

	encryptMsgCStr := C.CString(encryptMsg)
	defer C.free(unsafe.Pointer(encryptMsgCStr))

	msg := C.NewSlice()
	defer C.FreeSlice(msg)

	ret := C.DecryptData(encryptKeyCStr, encryptMsgCStr, msg)
	if ret != 0 {
		return "", getError(int(ret))
	}

	data := C.GoStringN(msg.buf, C.int(msg.len))
	return data, nil
}

// Destroy 销毁 SDK 实例
func (s *SDK) Destroy() {
	if s.sdk != nil {
		C.DestroySdk(s.sdk)
		s.sdk = nil
	}
}

// getError 根据错误码返回错误信息
func getError(code int) error {
	switch code {
	case ErrCodeInvalidParam:
		return errors.New("参数错误")
	case ErrCodeInvalidSecret:
		return errors.New("密钥错误")
	case ErrCodeDataDecryptFail:
		return errors.New("数据解密失败")
	case ErrCodeSystemFail:
		return errors.New("系统失败")
	case ErrCodeKeyDecryptFail:
		return errors.New("密钥解密失败")
	case ErrCodeFileIDError:
		return errors.New("fileid错误")
	case ErrCodeDecryptFail:
		return errors.New("解密失败")
	case ErrCodeKeyVersionError:
		return errors.New("找不到信息加密版本对应的私钥，需要重新下载私钥")
	case ErrCodeEncryptKeyError:
		return errors.New("解密encrypt_key失败")
	case ErrCodeIPForbidden:
		return errors.New("ip白名单")
	case ErrCodeDataExpired:
		return errors.New("数据过期")
	case ErrCodeCertError:
		return errors.New("证书错误")
	default:
		return errors.New("未知错误")
	}
}
