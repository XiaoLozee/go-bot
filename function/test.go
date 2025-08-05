package function

import (
	"github.com/XiaoLuozee/go-bot/botapi"
	"github.com/XiaoLuozee/go-bot/handler"
	"github.com/XiaoLuozee/go-bot/registry"
)

type TestPlugin struct{}

func (p *TestPlugin) Process(event interface{}) {
	msg, ok := event.(handler.OB11PrivateMessage)
	if !ok {
		return
	}

	// 2. 实现复读逻辑
	if msg.RawMessage == "测试" {
		//botapi.SendImgMsg(botapi.PrivateMessage, msg.UserID, "https://raw.gitcode.com/qq_44112897/images/raw/master/comic/14.jpg", botapi.WithSummary("最爱你了"))
		botapi.SendTextMsg(botapi.PrivateMessage, msg.UserID, "测试消息发送")
		//botapi.SendVideoMsg(botapi.PrivateMessage, msg.UserID, "https://upos-sz-estghw.bilivideo.com/upgcxcode/48/77/27701937748/27701937748-1-192.mp4?e=ig8euxZM2rNcNbRV7zdVhwdlhWdahwdVhoNvNC8BqJIzNbfq9rVEuxTEnE8L5F6VnEsSTx0vkX8fqJeYTj_lta53NCM=&nbs=1&uipk=5&platform=html5&trid=23f035f7e9db48ddbe661c1210db446h&deadline=1754381388&gen=playurlv3&og=hw&mid=0&oi=0x2408824c6c1fb780be2411fffe59c402&os=upos&upsig=202dda414e4600954c93db8b8967c5fc&uparams=e,nbs,uipk,platform,trid,deadline,gen,og,mid,oi,os&bvc=vod&nettype=0&bw=864845&agrr=1&buvid=&build=0&dl=0&f=h_0_0&orderid=0,1")
		//botapi.SendVideoMsg(botapi.GroupMessage, msg.GroupId, "https://upos-sz-estghw.bilivideo.com/upgcxcode/48/77/27701937748/27701937748-1-192.mp4?e=ig8euxZM2rNcNbRV7zdVhwdlhWdahwdVhoNvNC8BqJIzNbfq9rVEuxTEnE8L5F6VnEsSTx0vkX8fqJeYTj_lta53NCM=&nbs=1&uipk=5&platform=html5&trid=23f035f7e9db48ddbe661c1210db446h&deadline=1754381388&gen=playurlv3&og=hw&mid=0&oi=0x2408824c6c1fb780be2411fffe59c402&os=upos&upsig=202dda414e4600954c93db8b8967c5fc&uparams=e,nbs,uipk,platform,trid,deadline,gen,og,mid,oi,os&bvc=vod&nettype=0&bw=864845&agrr=1&buvid=&build=0&dl=0&f=h_0_0&orderid=0,1")
		//simpleNode := botapi.ForwardNode{
		//	Type: "node",
		//	Data: botapi.ForwardNodeData{
		//		UserID:   2010780496,
		//		Nickname: "强总",
		//		Content:  "给大家表演一个咬打火机",
		//	},
		//}
		//simpleNode2 := botapi.ForwardNode{
		//	Type: "node",
		//	Data: botapi.ForwardNodeData{
		//		UserID:   2010780496,
		//		Nickname: "强总",
		//		Content:  "草，走，忽略",
		//	},
		//}
		//allNodes := []botapi.ForwardNode{simpleNode, simpleNode2}
		//botapi.SendGroupForwardMsg(msg.GroupId, allNodes, botapi.WithSource("测试聊天记录"), botapi.WithForwardSummary("测试聊天记录2"), botapi.WithPrompt("[2条测试消息]"))
		//botapi.SendReplyMsg(botapi.PrivateMessage, msg.UserID, msg.MessageId, "测试哈哈")
	}
}

func init() {
	registry.RegisterFactory(
		registry.CreatePluginFactory(&TestPlugin{}, "bot.function.test", true),
	)
}
