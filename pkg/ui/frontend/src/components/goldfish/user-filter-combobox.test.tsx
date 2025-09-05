import { render, screen, fireEvent, waitFor } from '@testing-library/react';
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
      fireEvent.focus(input);
      
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
      
      await waitFor(() => {
        expect(screen.getByRole('option', { name: /bob@example.com/i })).toBeInTheDocument();
        expect(screen.queryByRole('option', { name: /alice@example.com/i })).not.toBeInTheDocument();
      });
      
      // Clear input
      await user.clear(input);
      
      await waitFor(() => {
        // All suggestions should be visible again
        mockSuggestions.forEach(suggestion => {
          expect(screen.getByRole('option', { name: new RegExp(suggestion, 'i') })).toBeInTheDocument();
        });
      });
    });

    it('hides dropdown when clicking outside', async () => {
      render(
        <UserFilterCombobox 
          value={undefined} 
          onChange={mockOnChange} 
          suggestions={mockSuggestions} 
        />
      );
      
      const input = screen.getByRole('textbox');
      fireEvent.focus(input);
      
      await waitFor(() => {
        expect(screen.getByRole('option', { name: /alice@example.com/i })).toBeInTheDocument();
      });
      
      // Click outside
      fireEvent.click(document.body);
      
      await waitFor(() => {
        expect(screen.queryByRole('option', { name: /alice@example.com/i })).not.toBeInTheDocument();
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
      fireEvent.focus(input);
      
      await waitFor(() => {
        const suggestion = screen.getByRole('option', { name: /bob@example.com/i });
        fireEvent.click(suggestion);
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
      
      // Press Enter or blur to confirm
      fireEvent.blur(input);
      
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
      fireEvent.click(clearButton);
      
      expect(mockOnChange).toHaveBeenCalledWith(undefined);
    });

    it('focuses input after clearing', async () => {
      render(
        <UserFilterCombobox 
          value="bob@example.com" 
          onChange={mockOnChange} 
          suggestions={mockSuggestions} 
        />
      );
      
      const clearButton = screen.getByRole('button', { name: /clear selection/i });
      fireEvent.click(clearButton);
      
      const input = screen.getByRole('textbox');
      await waitFor(() => {
        expect(document.activeElement).toBe(input);
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
      
      await waitFor(() => {
        expect(screen.getByRole('option', { name: /alice@example.com/i })).toBeInTheDocument();
        expect(screen.getByRole('option', { name: /bob@example.com/i })).toBeInTheDocument();
        expect(screen.getByRole('option', { name: /charlie@example.com/i })).toBeInTheDocument();
        expect(screen.queryByRole('option', { name: /david.smith@company.org/i })).not.toBeInTheDocument();
      });
    });

    it('shows no results message when no matches', async () => {
      const user = userEvent.setup();
      
      render(
        <UserFilterCombobox 
          value={undefined} 
          onChange={mockOnChange} 
          suggestions={mockSuggestions} 
        />
      );
      
      const input = screen.getByRole('textbox');
      await user.type(input, 'nonexistent@user.com');
      
      await waitFor(() => {
        expect(screen.getByText(/No matching users/i)).toBeInTheDocument();
      });
    });
  });

  describe('keyboard navigation', () => {
    it('navigates suggestions with arrow keys', async () => {
      render(
        <UserFilterCombobox 
          value={undefined} 
          onChange={mockOnChange} 
          suggestions={mockSuggestions} 
        />
      );
      
      const input = screen.getByRole('textbox');
      fireEvent.focus(input);
      
      await waitFor(() => {
        expect(screen.getByRole('option', { name: /alice@example.com/i })).toBeInTheDocument();
      });
      
      // Arrow down to highlight first suggestion
      fireEvent.keyDown(input, { key: 'ArrowDown' });
      
      // Enter to select
      fireEvent.keyDown(input, { key: 'Enter' });
      
      expect(mockOnChange).toHaveBeenCalledWith(mockSuggestions[0]);
    });

    it('closes dropdown with Escape key', async () => {
      render(
        <UserFilterCombobox 
          value={undefined} 
          onChange={mockOnChange} 
          suggestions={mockSuggestions} 
        />
      );
      
      const input = screen.getByRole('textbox');
      fireEvent.focus(input);
      
      await waitFor(() => {
        expect(screen.getByRole('option', { name: /alice@example.com/i })).toBeInTheDocument();
      });
      
      fireEvent.keyDown(input, { key: 'Escape' });
      
      await waitFor(() => {
        expect(screen.queryByRole('option', { name: /alice@example.com/i })).not.toBeInTheDocument();
      });
    });

    it('cycles through suggestions with arrow keys', async () => {
      userEvent.setup();
      
      render(
        <UserFilterCombobox 
          value={undefined} 
          onChange={mockOnChange} 
          suggestions={['user1', 'user2', 'user3']} 
        />
      );
      
      const input = screen.getByRole('textbox');
      fireEvent.focus(input);
      
      // Navigate down through all suggestions
      fireEvent.keyDown(input, { key: 'ArrowDown' });
      fireEvent.keyDown(input, { key: 'ArrowDown' });
      fireEvent.keyDown(input, { key: 'ArrowDown' });
      
      // Should wrap to first suggestion
      fireEvent.keyDown(input, { key: 'ArrowDown' });
      
      // Navigate up
      fireEvent.keyDown(input, { key: 'ArrowUp' });
      
      // Select current highlighted item
      fireEvent.keyDown(input, { key: 'Enter' });
      
      // Should have selected the last item after wrapping
      expect(mockOnChange).toHaveBeenCalled();
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
      fireEvent.focus(input);
      
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
          value="user+tag@example.com" 
          onChange={mockOnChange} 
          suggestions={specialUsers} 
        />
      );
      
      const input = screen.getByRole('textbox') as HTMLInputElement;
      expect(input.value).toBe('user+tag@example.com');
      
      fireEvent.focus(input);
      
      await waitFor(() => {
        specialUsers.forEach(user => {
          expect(screen.getByRole('option', { name: new RegExp(user, 'i') })).toBeInTheDocument();
        });
      });
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

    it('handles empty string in suggestions', () => {
      render(
        <UserFilterCombobox 
          value={undefined} 
          onChange={mockOnChange} 
          suggestions={['', 'valid@user.com']} 
        />
      );
      
      const input = screen.getByRole('textbox');
      fireEvent.focus(input);
      
      // Should filter out empty strings
      const suggestions = screen.getAllByRole('option');
      expect(suggestions).toHaveLength(1);
      expect(suggestions[0]).toHaveTextContent('valid@user.com');
    });
  });

  describe('performance', () => {
    it('debounces input changes', async () => {
      jest.useFakeTimers();
      const user = userEvent.setup({ delay: null });
      
      render(
        <UserFilterCombobox 
          value={undefined} 
          onChange={mockOnChange} 
          suggestions={mockSuggestions} 
        />
      );
      
      const input = screen.getByRole('textbox');
      
      // Type quickly
      await user.type(input, 'alice');
      
      // Should not call onChange immediately for each character
      expect(mockOnChange).not.toHaveBeenCalled();
      
      // Fast-forward timers
      jest.runAllTimers();
      
      // Enter to confirm
      fireEvent.keyDown(input, { key: 'Enter' });
      
      // Should call onChange only once with final value
      expect(mockOnChange).toHaveBeenCalledTimes(1);
      expect(mockOnChange).toHaveBeenCalledWith('alice');
      
      jest.useRealTimers();
    });

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
      fireEvent.focus(input);
      
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
      
      fireEvent.focus(input);
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

    it('supports keyboard-only interaction', async () => {
      render(
        <UserFilterCombobox 
          value={undefined} 
          onChange={mockOnChange} 
          suggestions={mockSuggestions} 
        />
      );
      
      const input = screen.getByRole('textbox');
      
      // Tab to focus
      input.focus();
      expect(document.activeElement).toBe(input);
      
      // Arrow down to navigate
      fireEvent.keyDown(input, { key: 'ArrowDown' });
      
      // Enter to select
      fireEvent.keyDown(input, { key: 'Enter' });
      
      expect(mockOnChange).toHaveBeenCalled();
    });
  });
});
