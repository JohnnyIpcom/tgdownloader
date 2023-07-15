package telegram

import "testing"

func TestFindHashtags(t *testing.T) {
	teststring := "This is a #test string with #hashtags"
	hashtags := findHashtags(teststring)
	if len(hashtags) != 2 {
		t.Errorf("Expected 2 hashtags, got %d", len(hashtags))
	}

	if hashtags[0] != "test" {
		t.Errorf("Expected test, got %s", hashtags[0])
	}

	if hashtags[1] != "hashtags" {
		t.Errorf("Expected hashtags, got %s", hashtags[1])
	}

	testCyrillicString := "This is a #тест string with #хэштеги"
	hashtags = findHashtags(testCyrillicString)
	if len(hashtags) != 2 {
		t.Errorf("Expected 2 hashtags, got %d", len(hashtags))
	}

	if hashtags[0] != "тест" {
		t.Errorf("Expected тест, got %s", hashtags[0])
	}

	if hashtags[1] != "хэштеги" {
		t.Errorf("Expected хэштеги, got %s", hashtags[1])
	}
}
