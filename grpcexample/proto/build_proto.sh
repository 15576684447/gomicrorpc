#!/usr/local/bin bash
protoc --proto_path=. --micro_out=. --go_out=. *.proto
#proto_path:指定对导入文件的搜索路径，如果不指定，默认为当前目录
#micro_out：xxx.micro.go文件输出目录，定义了api接口
#go_out:xxx.pb.go文件输出目录，定义了数据结构接口
#最后一项指定待编译文件