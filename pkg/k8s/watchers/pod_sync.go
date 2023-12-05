package watchers

func (k *K8sWatcher) SyncPodIpCache() {
	//logger := log.WithFields(logrus.Fields{
	//	"component": "SyncPodIpCache",
	//})
	//
	//for {
	//	time.Sleep(time.Minute)
	//	// 定期检查下当前节点下的ipcache的目标地址是否是对的
	//	err := ipcache.IPCache.DumpWithCallback(func(key bpf.MapKey, value bpf.MapValue) {
	//		lkey, ok := key.(*ipcache.Key)
	//		if !ok {
	//			logger.Errorf("get unsupported key %#v, %#v. ", key, value)
	//			return
	//		}
	//		lvalue, ok := value.(*ipcache.RemoteEndpointInfo)
	//		if !ok {
	//			logger.Errorf("get unsupported value %#v, %#v. ", key, value)
	//			return
	//		}
	//		logger.Debugf("begin check %s/%s ", lkey.String(), lvalue.String())
	//
	//		k.ciliumEndpointIndexer.Index()
	//	})
	//	if err != nil {
	//		logger.Errorf("dump ipcache failed %v. ", err)
	//		continue
	//	}
	//}

}
