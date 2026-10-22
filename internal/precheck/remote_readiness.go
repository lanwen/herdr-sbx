package precheck

func (i *Inspector) remoteReadiness() Check {
	return Check{"Sandbox setup", Unknown, "guest setup is checked only during permitted sandbox work", "For a running sandbox, choose Enable to add its herdr entry, or Re-sync to retry setup when available."}
}
