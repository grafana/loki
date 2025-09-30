import { render, screen, fireEvent, waitFor, act } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { UserFilterCombobox } from './user-filter-combobox';
import '@testing-library/jest-dom';

describe('UserFilterCombobox', () => {
  const mockOnChange = jest.fn();
  const mockSuggestions = [
    'alice@example.com',
    'bob@example.com',
    'charlie@example.com',
    'david.smith@company.org',
    'eve.jones@test.io'
  ];

  beforeEach(() => {
    jest.clearAllMocks();
  });

  describe('rendering', () => {
    it('renders with no selection', () => {
      render(
        <UserFilterCombobox 
          value={undefined} 
          onChange={mockOnChange} 
          suggestions={mockSuggestions} 
        />
      );
      
      const input = screen.getByRole('textbox');
      expect(input).toBeInTheDocument();
      expect(input).toHaveAttribute('placeholder', 'Filter by user...');
      expect(input).toHaveValue('');
    });

    it('renders with a selected user', () => {
      render(
        <UserFilterCombobox 
          value="alice@example.com" 
          onChange={mockOnChange} 
          suggestions={mockSuggestions} 
        />
      );
      
      const input = screen.getByRole('textbox') as HTMLInputElement;
      expect(input.value).toBe('alice@example.com');
    });

    it('renders with empty suggestions list', () => {
      render(
        <UserFilterCombobox 
          value={undefined} 
          onChange={mockOnChange} 
          suggestions={[]} 
        />
      );
      
      const input = screen.getByRole('textbox');
      expect(input).toBeInTheDocument();
    });
  });

  describe('autocomplete dropdown', () => {
    it('shows suggestions when input is focused', async () => {
      render(
        <UserFilterCombobox 
          value={undefined} 
          onChange={mockOnChange} 
          suggestions={mockSuggestions} 
        />
      );
      
      const input = screen.getByRole('textbox');
      act(() => {
        fireEvent.focus(input);
      });
      
      await waitFor(() => {
        mockSuggestions.forEach(user => {
          expect(screen.getByRole('option', { name: new RegExp(user, 'i') })).toBeInTheDocument();
        });
      });
    });

    it('filters suggestions based on input value', async () => {
      const user = userEvent.setup();
      
      render(
        <UserFilterCombobox 
          value={undefined} 
          onChange={mockOnChange} 
          suggestions={mockSuggestions} 
        />
      );
      
      const input = screen.getByRole('textbox');
      await user.type(input, 'alice');
      
      // Press Enter to apply filter immediately instead of waiting for debounce
      fireEvent.keyDown(input, { key: 'Enter' });

      // Now check immediately
      await waitFor(() => {
        expect(screen.getByRole('option', { name: /alice@example.com/i })).toBeInTheDocument();
        expect(screen.queryByRole('option', { name: /bob@example.com/i })).not.toBeInTheDocument();
        expect(screen.queryByRole('option', { name: /charlie@example.com/i })).not.toBeInTheDocument();
      });
    });

    it('shows all suggestions when typing then clearing input', async () => {
      const user = userEvent.setup();
      
      render(
        <UserFilterCombobox 
          value={undefined} 
          onChange={mockOnChange} 
          suggestions={mockSuggestions} 
        />
      );
      
      const input = screen.getByRole('textbox');
      
      // Type to filter
      await user.type(input, 'bob');
      
      // Press Enter to apply filter immediately
      fireEvent.keyDown(input, { key: 'Enter' });
      
      await waitFor(() => {
        expect(screen.getByRole('option', { name: /bob@example.com/i })).toBeInTheDocument();
        expect(screen.queryByRole('option', { name: /alice@example.com/i })).not.toBeInTheDocument();
      });
      
      // Clear input
      await user.clear(input);
      
      // Press Enter to apply clear immediately
      fireEvent.keyDown(input, { key: 'Enter' });
      
      await waitFor(() => {
        // All suggestions should be visible again
        mockSuggestions.forEach(suggestion => {
          expect(screen.getByRole('option', { name: new RegExp(suggestion, 'i') })).toBeInTheDocument();
        });
      });
    });
  });

  describe('selection', () => {
    it('selects a user from suggestions', async () => {
      render(
        <UserFilterCombobox 
          value={undefined} 
          onChange={mockOnChange} 
          suggestions={mockSuggestions} 
        />
      );
      
      const input = screen.getByRole('textbox');
      act(() => {
        fireEvent.focus(input);
      });
      
      await waitFor(() => {
        const suggestion = screen.getByRole('option', { name: /bob@example.com/i });
        act(() => {
          fireEvent.click(suggestion);
        });
      });
      
      expect(mockOnChange).toHaveBeenCalledWith('bob@example.com');
    });

    it('allows custom user input (not in suggestions)', async () => {
      const user = userEvent.setup();
      
      render(
        <UserFilterCombobox 
          value={undefined} 
          onChange={mockOnChange} 
          suggestions={mockSuggestions} 
        />
      );
      
      const input = screen.getByRole('textbox');
      await user.type(input, 'custom-user@domain.com');
      
      // Press Enter to confirm
      fireEvent.keyDown(input, { key: 'Enter' });
      
      expect(mockOnChange).toHaveBeenCalledWith('custom-user@domain.com');
    });

    it('clears selection when input is emptied', async () => {
      const user = userEvent.setup();
      
      render(
        <UserFilterCombobox 
          value="alice@example.com" 
          onChange={mockOnChange} 
          suggestions={mockSuggestions} 
        />
      );
      
      const input = screen.getByRole('textbox');
      await user.clear(input);
      
      // Press Enter to apply immediately (without waiting for debounce)
      fireEvent.keyDown(input, { key: 'Enter' });
      
      expect(mockOnChange).toHaveBeenCalledWith(undefined);
    });
  });

  describe('clear functionality', () => {
    it('shows clear button when a user is selected', () => {
      render(
        <UserFilterCombobox 
          value="alice@example.com" 
          onChange={mockOnChange} 
          suggestions={mockSuggestions} 
        />
      );
      
      const clearButton = screen.getByRole('button', { name: /clear selection/i });
      expect(clearButton).toBeInTheDocument();
    });

    it('does not show clear button when no user is selected', () => {
      render(
        <UserFilterCombobox 
          value={undefined} 
          onChange={mockOnChange} 
          suggestions={mockSuggestions} 
        />
      );
      
      expect(screen.queryByRole('button', { name: /clear selection/i })).not.toBeInTheDocument();
    });

    it('clears selection when clear button is clicked', () => {
      render(
        <UserFilterCombobox 
          value="bob@example.com" 
          onChange={mockOnChange} 
          suggestions={mockSuggestions} 
        />
      );
      
      const clearButton = screen.getByRole('button', { name: /clear selection/i });
      act(() => {
        fireEvent.click(clearButton);
      });
      
      expect(mockOnChange).toHaveBeenCalledWith(undefined);
    });

    it('clears value when clear button is clicked', async () => {
      render(
        <UserFilterCombobox 
          value="bob@example.com" 
          onChange={mockOnChange} 
          suggestions={mockSuggestions} 
        />
      );
      
      const clearButton = screen.getByRole('button', { name: /clear selection/i });
      act(() => {
        fireEvent.click(clearButton);
      });
      
      const input = screen.getByRole('textbox') as HTMLInputElement;
      await waitFor(() => {
        expect(input.value).toBe('');
        expect(mockOnChange).toHaveBeenCalledWith(undefined);
      });
    });
  });

  describe('filtering', () => {
    it('performs case-insensitive filtering', async () => {
      const user = userEvent.setup();
      
      render(
        <UserFilterCombobox 
          value={undefined} 
          onChange={mockOnChange} 
          suggestions={mockSuggestions} 
        />
      );
      
      const input = screen.getByRole('textbox');
      await user.type(input, 'ALICE');
      
      // Press Enter to apply filter immediately
      fireEvent.keyDown(input, { key: 'Enter' });
      
      await waitFor(() => {
        expect(screen.getByRole('option', { name: /alice@example.com/i })).toBeInTheDocument();
      });
    });

    it('matches partial strings', async () => {
      const user = userEvent.setup();
      
      render(
        <UserFilterCombobox 
          value={undefined} 
          onChange={mockOnChange} 
          suggestions={mockSuggestions} 
        />
      );
      
      const input = screen.getByRole('textbox');
      await user.type(input, 'example.com');
      
      // Press Enter to apply filter immediately
      fireEvent.keyDown(input, { key: 'Enter' });
      
      await waitFor(() => {
        expect(screen.getByRole('option', { name: /alice@example.com/i })).toBeInTheDocument();
        expect(screen.getByRole('option', { name: /bob@example.com/i })).toBeInTheDocument();
        expect(screen.getByRole('option', { name: /charlie@example.com/i })).toBeInTheDocument();
        expect(screen.queryByRole('option', { name: /david.smith@company.org/i })).not.toBeInTheDocument();
      });
    });

    it('closes dropdown when no matches found', async () => {
      const user = userEvent.setup();
      
      render(
        <UserFilterCombobox 
          value={undefined} 
          onChange={mockOnChange} 
          suggestions={mockSuggestions} 
        />
      );
      
      const input = screen.getByRole('textbox');
      
      // First open dropdown by typing a matching value
      await user.type(input, 'alice');
      fireEvent.keyDown(input, { key: 'Enter' });
      
      await waitFor(() => {
        expect(screen.getByText('alice@example.com')).toBeInTheDocument();
      });
      
      // Clear and type non-matching value - dropdown should close
      await user.clear(input);
      await user.type(input, 'nonexistent@user.com');
      fireEvent.keyDown(input, { key: 'Enter' });
      
      // Verify dropdown is closed (no suggestions visible)
      await waitFor(() => {
        expect(screen.queryByText('alice@example.com')).not.toBeInTheDocument();
      });
    });
  });

  describe('edge cases', () => {
    it('handles null onChange gracefully', () => {
      expect(() => {
        render(
          <UserFilterCombobox 
            value={undefined} 
            onChange={null as unknown as () => void} 
            suggestions={mockSuggestions} 
          />
        );
      }).not.toThrow();
    });

    it('handles duplicate suggestions', () => {
      const duplicates = ['user@test.com', 'user@test.com', 'other@test.com'];
      
      render(
        <UserFilterCombobox 
          value={undefined} 
          onChange={mockOnChange} 
          suggestions={duplicates} 
        />
      );
      
      const input = screen.getByRole('textbox');
      act(() => {
        fireEvent.focus(input);
      });
      
      // Should deduplicate suggestions
      const suggestions = screen.getAllByRole('option');
      expect(suggestions).toHaveLength(2);
    });

    it('handles special characters in user names', async () => {
      const specialUsers = [
        'user+tag@example.com',
        'user.name@example.com',
        'user_name@example.com',
        'user-name@example.com'
      ];
      
      render(
        <UserFilterCombobox 
          value={undefined} 
          onChange={mockOnChange} 
          suggestions={specialUsers} 
        />
      );
      
      const input = screen.getByRole('textbox') as HTMLInputElement;
      
      // Focus to show all suggestions
      act(() => {
        fireEvent.focus(input);
      });
      
      await waitFor(() => {
        specialUsers.forEach(user => {
          // Just check that the text is present, don't use regex with special chars
          expect(screen.getByText(user)).toBeInTheDocument();
        });
      });
      
      // Now test selecting one with special characters
      const specialOption = screen.getByText('user+tag@example.com');
      act(() => {
        fireEvent.click(specialOption);
      });
      
      expect(mockOnChange).toHaveBeenCalledWith('user+tag@example.com');
    });

    it('handles very long user names', () => {
      const longUser = 'a'.repeat(100) + '@example.com';
      
      render(
        <UserFilterCombobox 
          value={longUser} 
          onChange={mockOnChange} 
          suggestions={[longUser]} 
        />
      );
      
      const input = screen.getByRole('textbox') as HTMLInputElement;
      expect(input.value).toBe(longUser);
    });
  });

  describe('performance', () => {
    it('handles large suggestion lists', () => {
      const largeSuggestionList = Array.from({ length: 1000 }, (_, i) => `user${i}@example.com`);
      
      render(
        <UserFilterCombobox 
          value={undefined} 
          onChange={mockOnChange} 
          suggestions={largeSuggestionList} 
        />
      );
      
      const input = screen.getByRole('textbox');
      act(() => {
        fireEvent.focus(input);
      });
      
      // Should render without performance issues
      expect(input).toBeInTheDocument();
    });
  });

  describe('accessibility', () => {
    it('has proper ARIA attributes', () => {
      render(
        <UserFilterCombobox 
          value={undefined} 
          onChange={mockOnChange} 
          suggestions={mockSuggestions} 
        />
      );
      
      const input = screen.getByRole('textbox');
      expect(input).toHaveAttribute('aria-autocomplete', 'list');
      expect(input).toHaveAttribute('aria-expanded', 'false');
      
      act(() => {
        fireEvent.focus(input);
      });
      expect(input).toHaveAttribute('aria-expanded', 'true');
    });

    it('announces selected value to screen readers', () => {
      render(
        <UserFilterCombobox 
          value="alice@example.com" 
          onChange={mockOnChange} 
          suggestions={mockSuggestions} 
        />
      );
      
      const input = screen.getByRole('textbox');
      expect(input).toHaveValue('alice@example.com');
    });
  });
});
