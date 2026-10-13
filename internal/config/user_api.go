package config

func LoadUserDocument(path string) (UserDocument, error) {
	manager, err := OpenUserDocument(path)
	if err != nil {
		return UserDocument{}, err
	}
	return manager.Snapshot(), nil
}

func SaveUserDocument(path, content string) (UserDocument, error) {
	manager, err := OpenUserDocument(path)
	if err != nil {
		return UserDocument{}, err
	}
	return manager.Save(content)
}
