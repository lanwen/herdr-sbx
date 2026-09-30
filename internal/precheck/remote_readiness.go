package precheck

func (i *Inspector) remoteReadiness() Check {
	return Check{"Remote readiness", Unknown, "checked only during explicit registration", "Choose Register prepared running sandboxes to verify a remote Herdr server."}
}
