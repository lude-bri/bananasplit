/* JSON STORE

Author: Luigi Piantavinha

2026jun15 - Initial Version

*/

package store

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"sync"

	"bananasplit/internal/app"
)

/* *****************************************************************************
*
* Structs
*
*******************************************************************************/

type JSONStore struct {
	path string
	mu   sync.Mutex
}

/* *****************************************************************************
*
* Functions
*
*******************************************************************************/

/* NewJSONStore - creates a new JSONStore
*
* This function initializes a new JSONStore by creating the necessary
* directories and an empty JSON file if it doesn't already exist.
*
* Parameters:
*   - path: The file system path where the JSONStore will be created.
*
* Returns:
*   - A pointer to the newly created JSONStore, or an error if the creation
*     failed.
 */

func NewJSONStore(path string) (*JSONStore, error) {

	/* Creating directories if needed */
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}

	/* Check for existing file */
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {

		/* Create file with empty array */
		if err := os.WriteFile(path, []byte("[]\n"), 0o644); err != nil {
			return nil, err
		}
	}

	return &JSONStore{path: path}, nil
}

/* All - retrieves all expenses from the store
*
* This function loads all expenses from the JSON file and returns them as a slice.
*
* Parameters:
*   - None
*
* Returns:
*   - A slice of app.Expense containing all expenses, or an error if the
*     loading failed.
 */

func (s *JSONStore) All() ([]app.Expense, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.load()
}

/* Add - adds a new expense to the store
*
* This function appends a new expense to the JSON file.
*
* Parameters:
*   - expense: The app.Expense to add.
*
* Returns:
*   - The added app.Expense, or an error if the addition failed.
 */

func (s *JSONStore) Add(expense app.Expense) (app.Expense, error) {

	s.mu.Lock()
	defer s.mu.Unlock()

	expenses, err := s.load()
	if err != nil {
		return app.Expense{}, err
	}

	expense.ID = nextID(expenses)
	expenses = append(expenses, expense)

	if err := s.save(expenses); err != nil {
		return app.Expense{}, err
	}

	return expense, nil
}

func (s *JSONStore) Delete(id int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	expenses, err := s.load()
	if err != nil {
		return err
	}

	expenses = slices.DeleteFunc(expenses, func(expense app.Expense) bool {
		return expense.ID == id
	})

	return s.save(expenses)
}

func (s *JSONStore) load() ([]app.Expense, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		return nil, err
	}

	var expenses []app.Expense
	if err := json.Unmarshal(data, &expenses); err != nil {
		return nil, err
	}

	return expenses, nil
}

func (s *JSONStore) save(expenses []app.Expense) error {
	data, err := json.MarshalIndent(expenses, "", "  ")
	if err != nil {
		return err
	}

	data = append(data, '\n')
	return os.WriteFile(s.path, data, 0o644)
}

func nextID(expenses []app.Expense) int64 {
	var maxID int64
	for _, expense := range expenses {
		if expense.ID > maxID {
			maxID = expense.ID
		}
	}

	return maxID + 1
}
