# QQGo

用 Go 和 [MyGo](https://github.com/egoist/mygo) 写的第三方 QQ 音乐桌面客户端，原生界面，无 WebView。

界面、播放器与歌词动画来自 [MyGo 云音乐](https://github.com/egoist/mygo-cloudmusic)，QQ 音乐的协议移植自 [Lyrune](https://github.com/amtoaer/lyrune)。

## 功能

- 扫码登录：QQ 音乐 App、QQ、微信三种方式，登录态保存在系统凭据存储中，到期前自动续期
- 每日 30 首（未登录时为热歌榜）、推荐歌单、巅峰榜与全部排行榜、我的歌单与收藏的歌单
- 搜索单曲、歌手、专辑与歌单，输入时显示搜索建议，保留搜索历史
- 歌手页的热门与最新歌曲、专辑，关注歌手；专辑页
- 个性电台：猜你喜欢、专属雷达，一首接一首播放；不喜欢的歌可跳过并告知 QQ 音乐
- 边下边播，支持标准 / HQ / SQ 无损（FLAC）/ Hi-Res 音质，按账号可用的最高音质回退；CDN 节点自动切换
- 听过的歌曲缓存在本机，再次播放无需等待
- 按 QQ 音乐测得的响度做音量均衡，暂停与继续时淡出淡入，拖动进度无杂音
- 退出后再打开，从上次的播放队列与进度继续
- 逐字歌词（QRC）与翻译，正在播放页的「律动」视图与「选歌」封面墙
- 喜欢歌曲、新建与删除歌单、添加与移除歌曲、播放队列、随机与循环播放
- 浅色 / 深色 / 跟随系统，媒体键与系统「正在播放」，托盘图标

未登录时可以浏览全部内容，并播放免费歌曲；VIP 歌曲与更高音质需要登录（及绿钻）。

## 构建

需要 Go 1.27+。

```bash
go tool mygo dev     # 开发模式运行
go tool mygo build   # 打包到 build/（Linux 下另有 .deb 与带 install.sh 的 tar.gz）
go test ./...        # 运行测试；设置 LIVE=1 时另外访问 QQ 音乐做联网测试
```

## 目录结构

```
main.go                入口
mygo.json              应用名称、标识、版本与打包配置
internal/qqmusic/      QQ 音乐协议：签名网关、扫码登录（含 MQTT）、曲库、播放地址与 CDN、QRC 歌词解密
internal/player/       播放器：边下边播、MP3/FLAC 解码、缓存、淡入淡出、频谱
internal/lyrics/       歌词解析：QRC 逐字歌词、LRC 与翻译
internal/app/          界面与应用状态（MyGo 原生 UI），debug_shots.go 为截图检查工具
icon/                  图标源文件与生成程序
resources/             打包时附带的图标
scripts/reset.sh       清除本机的设置、播放进度与登录，恢复首次打开的状态
```

## 致谢

- [MyGo](https://github.com/egoist/mygo)：原生 UI 与应用框架
- [MyGo 云音乐](https://github.com/egoist/mygo-cloudmusic)：本项目在其基础上改写（AGPL-3.0）
- [Lyrune](https://github.com/amtoaer/lyrune) 与 [netease-qq-music-api](https://github.com/AstronW/netease-qq-music-api)：QQ 音乐的签名、扫码登录、播放地址与 CDN 调度
- [jixunmoe-go/qrc](https://github.com/jixunmoe-go/qrc)：QRC 歌词的解密算法
- [QQMusicApi](https://github.com/L-1124/QQMusicApi)：歌手、专辑、歌单编辑等接口的参考

第三方代码的许可见 [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md)。

本项目与腾讯及 QQ 音乐官方无关，音乐版权归各权利人所有。请遵守当地法律与 QQ 音乐的服务条款。

## 许可证

[AGPL-3.0](LICENSE)
