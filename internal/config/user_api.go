package config

// LoadUserDocument reads a validated external user.md snapshot.
func LoadUserDocument(path string) (UserDocument, error) {
	manager, err := OpenUserDocument(path)
	if err != nil {
		return UserDocument{}, err
	}
	return manager.Snapshot(), nil
}

// SaveUserDocument performs a validated atomic write and increments revision.
func SaveUserDocument(path, content string) (UserDocument, error) {
	manager, err := OpenUserDocument(path)
	if err != nil {
		return UserDocument{}, err
	}
	return manager.Save(content)
}
