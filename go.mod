module cos-archive

go 1.14

require (
	github.com/allan-simon/go-singleinstance v0.0.0-20210120080615-d0997106ab37
	github.com/dustin/go-humanize v1.0.0
	github.com/google/go-cmp v0.5.5 // indirect
	github.com/google/go-querystring v1.1.0 // indirect
	github.com/google/uuid v1.2.0 // indirect
	github.com/kr/text v0.2.0 // indirect
	github.com/mozillazg/go-httpheader v0.3.0 // indirect
	github.com/stretchr/testify v1.7.0 // indirect
	github.com/tencentyun/cos-go-sdk-v5 v0.7.24
	github.com/ugorji/go/codec v1.2.5
	go.uber.org/ratelimit v0.2.0
	golang.org/x/xerrors v0.0.0-20200804184101-5ec99f83aff1 // indirect
	gopkg.in/check.v1 v1.0.0-20201130134442-10cb98267c6c // indirect
	gopkg.in/yaml.v3 v3.0.0-20210107192922-496545a6307b // indirect
)

replace github.com/tencentyun/cos-go-sdk-v5 => github.com/yuhuan417/cos-go-sdk-v5 v0.7.22-yuhuan417
