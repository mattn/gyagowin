package main

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
	"unicode/utf16"
	"unsafe"

	"github.com/cwchiu/go-winapi"
)

const (
	MAX_PATH     = 260
	LWA_COLORKEY = 0x00001
	LWA_ALPHA    = 0x00002
)

var (
	onClip    = false
	firstDraw = true
	clipRect  = winapi.RECT{0, 0, 0, 0}
	lastRect  = winapi.RECT{0, 0, 0, 0}
	ofX, ofY  int32
	hLayerWnd winapi.HWND

	modgdi32                       = syscall.NewLazyDLL("gdi32.dll")
	moduser32                      = syscall.NewLazyDLL("user32.dll")
	procCreateCompatibleBitmap     = modgdi32.NewProc("CreateCompatibleBitmap")
	procSetLayeredWindowAttributes = moduser32.NewProc("SetLayeredWindowAttributes")

	modgdiplus              = syscall.NewLazyDLL("gdiplus.dll")
	procGdipSaveImageToFile = modgdiplus.NewProc("GdipSaveImageToFile")

	modole32            = syscall.NewLazyDLL("ole32.dll")
	procCLSIDFromString = modole32.NewProc("CLSIDFromString")
)

type EncoderParameter struct {
	Guid           winapi.GUID
	NumberOfValues uint32
	TypeAPI        uint32
	Value          uintptr
}

type EncoderParameters struct {
	Count     uint32
	Parameter [1]EncoderParameter
}

func drawRubberband(hdc winapi.HDC, newRect *winapi.RECT, erase bool) {

	if firstDraw {
		// レイヤーウィンドウを表示
		winapi.ShowWindow(hLayerWnd, winapi.SW_SHOW)
		winapi.UpdateWindow(hLayerWnd)

		firstDraw = false
	}

	if erase {
		// レイヤーウィンドウを隠す
		winapi.ShowWindow(hLayerWnd, winapi.SW_HIDE)

	}

	// 座標チェック
	clipRect = *newRect
	if clipRect.Right < clipRect.Left {
		clipRect.Left, clipRect.Right = clipRect.Right, clipRect.Left
	}
	if clipRect.Bottom < clipRect.Top {
		clipRect.Top, clipRect.Bottom = clipRect.Bottom, clipRect.Top
	}
	winapi.MoveWindow(hLayerWnd, clipRect.Left, clipRect.Top,
		clipRect.Right-clipRect.Left+1, clipRect.Bottom-clipRect.Top+1, true)

	return
}

func savePNG(fileName string, newBMP winapi.HBITMAP) bool {
	res := false

	var gdiplusStartupInput winapi.GdiplusStartupInput
	var gdiplusToken winapi.GdiplusStartupOutput

	// GDI+ の初期化
	gdiplusStartupInput.GdiplusVersion = 1
	winapi.GdiplusStartup(&gdiplusStartupInput, &gdiplusToken)

	// HBITMAP から Bitmap を作成
	var bmp *winapi.GpBitmap
	if winapi.GdipCreateBitmapFromHBITMAP(newBMP, 0, &bmp) == 0 {
		sclsid, _ := syscall.UTF16PtrFromString("{557CF406-1A04-11D3-9A73-0000F81EF32E}")
		if clsid, err := CLSIDFromString(sclsid); err == nil {
			fname, _ := syscall.UTF16PtrFromString(fileName)
			if GdipSaveImageToFile(bmp, fname, clsid, nil) == 0 {
				res = true
			}
			winapi.GdipDisposeImage((*winapi.GpImage)(bmp))
		}
	}

	// 後始末
	winapi.GdiplusShutdown()

	return res
}

func uploadFile(hWnd winapi.HWND, fileName string) bool {
	endpoint := os.Getenv("GYAGO_SERVER")
	if endpoint == "" {
		endpoint = "http://gyazo.com/upload.cgi"
	}

	// get hostname for filename
	url_, err := url.Parse(endpoint)
	if err != nil {
		return false
	}
	host := strings.SplitN(url_.Host, ":", 2)[0]

	// make content
	content, err := ioutil.ReadFile(fileName)
	if err != nil {
		return false
	}

	// %Y%m%d%H%M%S
	id := time.Now().Format("20060102030405")

	// create multipart
	var b bytes.Buffer
	w := multipart.NewWriter(&b)
	err = w.WriteField("id", id)
	part, err := w.CreateFormFile("imagedata", host)
	if err != nil {
		return false
	}
	part.Write(content)
	err = w.Close()
	if err != nil {
		return false
	}
	body := strings.NewReader(b.String())

	// then, upload
	res, err := http.Post(endpoint, w.FormDataContentType(), body)
	if err != nil {
		return false
	}
	defer res.Body.Close()

	content, err = ioutil.ReadAll(res.Body)
	if err != nil {
		return false
	}
	return exec.Command("rundll32.exe", "url.dll,FileProtocolHandler", string(content)).Run() == nil
}

func toUTF16(s string) (*uint16, int32) {
	tx := utf16.Encode([]rune(s))
	return (*uint16)(unsafe.Pointer(&tx[0])), int32(len(tx))
}

func CreateCompatibleBitmap(hdc winapi.HDC, width, height int32) winapi.HGDIOBJ {
	r0, _, _ := syscall.Syscall(procCreateCompatibleBitmap.Addr(), 3, uintptr(hdc), uintptr(width), uintptr(height))
	return winapi.HGDIOBJ(r0)
}

func SetLayeredWindowAttributes(hwnd winapi.HWND, cr winapi.COLORREF, alpha byte, flags uint32) bool {
	r0, _, _ := syscall.Syscall6(procSetLayeredWindowAttributes.Addr(), 4, uintptr(hwnd), uintptr(cr), uintptr(alpha), uintptr(flags), 0, 0)
	return r0 != 0
}

func CLSIDFromString(str *uint16) (clsid *winapi.GUID, err error) {
	var guid winapi.GUID
	err = nil

	hr, _, _ := syscall.Syscall(procCLSIDFromString.Addr(), 2, uintptr(unsafe.Pointer(str)), uintptr(unsafe.Pointer(&guid)), 0)
	if hr != 0 {
		err = syscall.Errno(hr)
	}

	clsid = &guid
	return
}

func GdipSaveImageToFile(image *winapi.GpBitmap, filename *uint16, clsidEncoder *winapi.GUID, encoderParams *EncoderParameters) winapi.GpStatus {
	ret, _, _ := syscall.Syscall6(procGdipSaveImageToFile.Addr(), 4, uintptr(unsafe.Pointer(image)), uintptr(unsafe.Pointer(filename)), uintptr(unsafe.Pointer(clsidEncoder)), uintptr(unsafe.Pointer(encoderParams)), 0, 0)
	return winapi.GpStatus(ret)
}

func WndProc(hWnd winapi.HWND, message uint32, wParam uintptr, lParam uintptr) uintptr {
	var hdc winapi.HDC

	switch message {
	case winapi.WM_RBUTTONDOWN:
		// キャンセル
		winapi.DestroyWindow(hWnd)
		return winapi.DefWindowProc(hWnd, message, wParam, lParam)

	case winapi.WM_TIMER:
		// ESCキー押下の検知
		if int(winapi.GetKeyState(winapi.VK_ESCAPE))&0x8000 != 0 {
			winapi.DestroyWindow(hWnd)
			return winapi.DefWindowProc(hWnd, message, wParam, lParam)
		}
		break

	case winapi.WM_MOUSEMOVE:
		if onClip {
			// 新しい座標をセット
			clipRect.Right = int32(winapi.LOWORD(uint32(lParam))) + ofX
			clipRect.Bottom = int32(winapi.HIWORD(uint32(lParam))) + ofY

			hdc = winapi.GetDC(0)
			drawRubberband(hdc, &clipRect, false)

			winapi.ReleaseDC(0, hdc)
		}
		break

	case winapi.WM_LBUTTONDOWN:
		{
			// クリップ開始
			onClip = true

			// 初期位置をセット
			clipRect.Left = int32(winapi.LOWORD(uint32(lParam))) + ofX
			clipRect.Top = int32(winapi.HIWORD(uint32(lParam))) + ofY

			// マウスをキャプチャ
			winapi.SetCapture(hWnd)
		}
		break

	case winapi.WM_LBUTTONUP:
		{
			// クリップ終了
			onClip = false

			// マウスのキャプチャを解除
			winapi.ReleaseCapture()

			clipRect.Right = int32(winapi.LOWORD(uint32(lParam))) + ofX
			clipRect.Bottom = int32(winapi.HIWORD(uint32(lParam))) + ofY

			hdc := winapi.GetDC(0)

			// 線を消す
			drawRubberband(hdc, &clipRect, true)

			// 座標チェック
			if clipRect.Right < clipRect.Left {
				clipRect.Left, clipRect.Right = clipRect.Right, clipRect.Left
			}
			if clipRect.Bottom < clipRect.Top {
				clipRect.Top, clipRect.Bottom = clipRect.Bottom, clipRect.Top
			}

			// 画像のキャプチャ
			iWidth := (clipRect.Right - clipRect.Left + 1)
			iHeight := (clipRect.Bottom - clipRect.Top + 1)

			if iWidth == 0 || iHeight == 0 {
				// 画像になってない, なにもしない
				winapi.ReleaseDC(0, hdc)
				winapi.DestroyWindow(hWnd)
				break
			}

			// ビットマップバッファを作成
			newBMP := CreateCompatibleBitmap(hdc, iWidth, iHeight)
			newDC := winapi.CreateCompatibleDC(hdc)

			// 関連づけ
			winapi.SelectObject(newDC, newBMP)

			// 画像を取得
			winapi.BitBlt(newDC, 0, 0, iWidth, iHeight, hdc, clipRect.Left, clipRect.Top, winapi.SRCCOPY)

			// ウィンドウを隠す!
			winapi.ShowWindow(hWnd, winapi.SW_HIDE)

			// テンポラリファイル名を決定
			tmpFile, _ := ioutil.TempFile("", "gya")
			tmpFile.Close()
			fileName := tmpFile.Name()

			if savePNG(fileName, winapi.HBITMAP(newBMP)) {
				if !uploadFile(hWnd, fileName) {
				}
				println(fileName)
			} else {
				// PNG保存失敗...
				errmsg, _ := syscall.UTF16PtrFromString("Cannot save png image")
				errtitle, _ := syscall.UTF16PtrFromString("Gyago")
				winapi.MessageBox(hWnd, errmsg, errtitle, winapi.MB_OK|winapi.MB_ICONERROR)
			}

			// 後始末
			os.Remove(fileName)

			winapi.DeleteDC(newDC)
			winapi.DeleteObject(newBMP)

			winapi.ReleaseDC(0, hdc)
			winapi.DestroyWindow(hWnd)
			winapi.PostQuitMessage(0)
		}
		break

	case winapi.WM_DESTROY:
		winapi.PostQuitMessage(0)
		break

	default:
		return winapi.DefWindowProc(hWnd, message, wParam, lParam)
	}
	return 0
}

func LayerWndProc(hWnd winapi.HWND, message uint32, wParam uintptr, lParam uintptr) uintptr {
	var hdc winapi.HDC
	clipRect := winapi.RECT{0, 0, 500, 500}
	var hBrush winapi.HBRUSH
	var hPen winapi.HPEN
	var hFont winapi.HFONT

	switch message {
	case winapi.WM_ERASEBKGND:
		winapi.GetClientRect(hWnd, &clipRect)

		hdc = winapi.GetDC(hWnd)
		hBrush = winapi.CreateSolidBrush(0x646464)
		winapi.SelectObject(hdc, winapi.HGDIOBJ(hBrush))
		hPen = winapi.CreatePen(winapi.PS_DASH, 1, 0xffffff)
		winapi.SelectObject(hdc, winapi.HGDIOBJ(hPen))
		winapi.Rectangle(hdc, 0, 0, clipRect.Right, clipRect.Bottom)

		fontnm, _ := syscall.UTF16PtrFromString("Tahoma")

		//矩形のサイズを出力
		fHeight := -winapi.MulDiv(8, winapi.GetDeviceCaps(hdc, winapi.LOGPIXELSY), 72)
		hFont = winapi.CreateFont(fHeight, //フォント高さ
			0,                                   //文字幅
			0,                                   //テキストの角度
			0,                                   //ベースラインとｘ軸との角度
			winapi.FW_REGULAR,                   //フォントの重さ（太さ）
			0,                                   //イタリック体
			0,                                   //アンダーライン
			0,                                   //打ち消し線
			winapi.ANSI_CHARSET,                 //文字セット
			winapi.OUT_DEFAULT_PRECIS,           //出力精度
			winapi.CLIP_DEFAULT_PRECIS,          //クリッピング精度
			winapi.PROOF_QUALITY,                //出力品質
			winapi.FIXED_PITCH|winapi.FF_MODERN, //ピッチとファミリー
			fontnm) //書体名

		winapi.SelectObject(hdc, winapi.HGDIOBJ(hFont))

		var iWidth, iHeight int
		iWidth = int(clipRect.Right - clipRect.Left)
		iHeight = int(clipRect.Bottom - clipRect.Top)

		sWidth, lWidth := toUTF16(fmt.Sprintf("%d", iWidth))
		sHeight, lHeight := toUTF16(fmt.Sprintf("%d", iHeight))

		w := int32(-float64(fHeight)*2.5 + 8)
		h := int32(-fHeight*2 + 8)
		h2 := h + fHeight

		winapi.SetBkMode(hdc, winapi.TRANSPARENT)
		winapi.SetTextColor(hdc, 0x0)
		winapi.TextOut(hdc, clipRect.Right-w+1, clipRect.Bottom-h+1, sWidth, lWidth)
		winapi.TextOut(hdc, clipRect.Right-w+1, clipRect.Bottom-h2+1, sHeight, lHeight)
		winapi.SetTextColor(hdc, 0xffffff)
		winapi.TextOut(hdc, clipRect.Right-w, clipRect.Bottom-h, sWidth, lWidth)
		winapi.TextOut(hdc, clipRect.Right-w, clipRect.Bottom-h2, sHeight, lHeight)

		winapi.DeleteObject(winapi.HGDIOBJ(hPen))
		winapi.DeleteObject(winapi.HGDIOBJ(hBrush))
		winapi.DeleteObject(winapi.HGDIOBJ(hFont))
		winapi.ReleaseDC(hWnd, hdc)

		return 1

		break
	default:
		return winapi.DefWindowProc(hWnd, message, wParam, lParam)
	}
	return 0
}

func MyRegisterClass(hInstance winapi.HINSTANCE) winapi.ATOM {
	var wc winapi.WNDCLASSEX

	wc.CbSize = uint32(unsafe.Sizeof(winapi.WNDCLASSEX{}))
	wc.Style = 0
	wc.LpfnWndProc = syscall.NewCallback(WndProc)
	wc.CbClsExtra = 0
	wc.CbWndExtra = 0
	wc.HInstance = hInstance
	wc.HIcon = winapi.LoadIcon(hInstance, winapi.MAKEINTRESOURCE(132))
	wc.HCursor = winapi.LoadCursor(0, winapi.MAKEINTRESOURCE(winapi.IDC_CROSS))
	wc.HbrBackground = 0
	wc.LpszMenuName = nil
	wc.LpszClassName, _ = syscall.UTF16PtrFromString("GYAZOWIN")

	winapi.RegisterClassEx(&wc)

	var lwc winapi.WNDCLASSEX
	lwc.CbSize = uint32(unsafe.Sizeof(winapi.WNDCLASSEX{}))
	lwc.Style = winapi.CS_HREDRAW | winapi.CS_VREDRAW
	lwc.LpfnWndProc = syscall.NewCallback(LayerWndProc)
	lwc.CbClsExtra = 0
	lwc.CbWndExtra = 0
	lwc.HInstance = hInstance
	lwc.HIcon = winapi.LoadIcon(hInstance, winapi.MAKEINTRESOURCE(132))
	lwc.HCursor = winapi.LoadCursor(0, winapi.MAKEINTRESOURCE(winapi.IDC_CROSS))
	lwc.HbrBackground = winapi.HBRUSH(winapi.GetStockObject(winapi.WHITE_BRUSH))
	lwc.LpszMenuName = nil
	lwc.LpszClassName, _ = syscall.UTF16PtrFromString("GYAZOWINL")

	return winapi.RegisterClassEx(&lwc)
}

func InitInstance(hInstance winapi.HINSTANCE, nCmdShow int) bool {
	x := winapi.GetSystemMetrics(winapi.SM_XVIRTUALSCREEN)
	y := winapi.GetSystemMetrics(winapi.SM_YVIRTUALSCREEN)
	w := winapi.GetSystemMetrics(winapi.SM_CXVIRTUALSCREEN)
	h := winapi.GetSystemMetrics(winapi.SM_CYVIRTUALSCREEN)

	ofX, ofY = x, y

	clazz, _ := syscall.UTF16PtrFromString("GYAZOWIN")

	hWnd := winapi.CreateWindowEx(
		winapi.WS_EX_TRANSPARENT|winapi.WS_EX_TOOLWINDOW|winapi.WS_EX_TOPMOST|winapi.WS_EX_NOACTIVATE,
		clazz, nil, winapi.WS_POPUP,
		0, 0, 0, 0,
		0, 0, hInstance, nil)
	if hWnd == 0 {
		return false
	}

	winapi.MoveWindow(hWnd, x, y, w, h, false)

	winapi.ShowWindow(hWnd, winapi.SW_SHOW)
	winapi.UpdateWindow(hWnd)

	winapi.SetTimer(hWnd, 1, 100, 0)

	clazz, _ = syscall.UTF16PtrFromString("GYAZOWINL")

	// レイヤーウィンドウの作成
	hLayerWnd = winapi.CreateWindowEx(
		winapi.WS_EX_TOOLWINDOW|winapi.WS_EX_LAYERED|winapi.WS_EX_NOACTIVATE,
		clazz, nil, winapi.WS_POPUP,
		100, 100, 300, 300,
		hWnd, 0, hInstance, nil)

	SetLayeredWindowAttributes(hLayerWnd, 0xff0000, 100, LWA_COLORKEY|LWA_ALPHA)
	return true
}

func main() {
	hInstance := winapi.GetModuleHandle(nil)

	MyRegisterClass(hInstance)

	if InitInstance(hInstance, winapi.SW_SHOW) == false {
		return
	}

	var msg winapi.MSG
	for winapi.GetMessage(&msg, 0, 0, 0) != 0 {
		winapi.TranslateMessage(&msg)
		winapi.DispatchMessage(&msg)
	}

	os.Exit(int(msg.WParam))
}
