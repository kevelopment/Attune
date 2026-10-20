package library

import "attune/internal/model"

// GetBook returns a book record and its imported format assets, including
// records that currently have no EPUB.
func (s *Store) GetBook(id string) (model.Book, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	book, err := s.get(id)
	if err != nil {
		return model.Book{}, err
	}
	book.Files, err = s.BookFiles(id)
	if err != nil {
		return model.Book{}, err
	}
	for _, file := range book.Files {
		if file.Preferred {
			book.PreferredFileID = file.ID
			break
		}
	}
	return book, nil
}
