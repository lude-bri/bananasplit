/* JSON STORE

Author: Luigi Piantavinha

2026jun15 - Initial Version

*/

package store

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
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

	expense, err := s.load()
	if err != nil {
		return app.Expense{}, err
	}

}
