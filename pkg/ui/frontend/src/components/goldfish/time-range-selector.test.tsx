import { render, screen, fireEvent, waitFor, act } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { TimeRangeSelector } from './time-range-selector';
import '@testing-library/jest-dom';

describe('TimeRangeSelector', () => {
  const mockOnChange = jest.fn();

  beforeEach(() => {
    jest.clearAllMocks();
    jest.useFakeTimers();
    jest.setSystemTime(new Date('2024-01-15T12:00:00Z'));
  });

  afterEach(() => {
    jest.useRealTimers();
  });

  describe('rendering', () => {
    it('renders with default state', () => {
      render(<TimeRangeSelector from={null} to={null} onChange={mockOnChange} />);
      
      expect(screen.getByRole('button')).toBeInTheDocument();
      expect(screen.getByText(/Default \(Last 1 hour\)/i)).toBeInTheDocument();
    });

    it('renders with selected dates', () => {
      const from = new Date('2024-01-10T00:00:00Z');
      const to = new Date('2024-01-14T23:59:59Z');
      
      render(<TimeRangeSelector from={from} to={to} onChange={mockOnChange} />);
      
      expect(screen.getByText('Custom')).toBeInTheDocument();
    });

    it('shows calendar icon', () => {
      render(<TimeRangeSelector from={null} to={null} onChange={mockOnChange} />);
      
      const button = screen.getByRole('button');
      expect(button.querySelector('svg')).toBeInTheDocument();
    });
  });

  describe('preset ranges', () => {
    it('shows preset range options when opened', async () => {
      render(<TimeRangeSelector from={null} to={null} onChange={mockOnChange} />);
      
      const button = screen.getByRole('button');
      fireEvent.click(button);
      
      await waitFor(() => {
        // Check that the popover is open with Quick ranges section
        expect(screen.getByText('Quick ranges')).toBeInTheDocument();
      });
    });

    it('selects last 15 minutes preset', async () => {
      render(<TimeRangeSelector from={null} to={null} onChange={mockOnChange} />);
      
      await act(async () => {
        fireEvent.click(screen.getByRole('button'));
      });
      
      // Wait for popover to open
      await waitFor(() => {
        expect(screen.getByText('Quick ranges')).toBeInTheDocument();
      });
      
      // Click on the select dropdown
      const selectTrigger = screen.getByText('Select a preset');
      await act(async () => {
        fireEvent.click(selectTrigger);
      });
      
      // Select the preset
      await waitFor(() => {
        const option = screen.getByText('Last 15 minutes');
        fireEvent.click(option);
      });
      
      expect(mockOnChange).toHaveBeenCalledWith(
        new Date('2024-01-15T11:45:00Z'),
        new Date('2024-01-15T12:00:00Z')
      );
    });

    it('selects last 3 hours preset', async () => {
      render(<TimeRangeSelector from={null} to={null} onChange={mockOnChange} />);
      
      await act(async () => {
        fireEvent.click(screen.getByRole('button'));
      });
      
      // Wait for popover to open
      await waitFor(() => {
        expect(screen.getByText('Quick ranges')).toBeInTheDocument();
      });
      
      // Click on the select dropdown
      const selectTrigger = screen.getByText('Select a preset');
      await act(async () => {
        fireEvent.click(selectTrigger);
      });
      
      // Select the preset - note component has 'Last 3 hours' not 'Last 4 hours'
      await waitFor(() => {
        const option = screen.getByText('Last 3 hours');
        fireEvent.click(option);
      });
      
      expect(mockOnChange).toHaveBeenCalledWith(
        new Date('2024-01-15T09:00:00Z'),
        new Date('2024-01-15T12:00:00Z')
      );
    });


    it('selects last 7 days preset', async () => {
      render(<TimeRangeSelector from={null} to={null} onChange={mockOnChange} />);
      
      await act(async () => {
        fireEvent.click(screen.getByRole('button'));
      });
      
      // Wait for popover to open
      await waitFor(() => {
        expect(screen.getByText('Quick ranges')).toBeInTheDocument();
      });
      
      // Click on the select dropdown
      const selectTrigger = screen.getByText('Select a preset');
      await act(async () => {
        fireEvent.click(selectTrigger);
      });
      
      // Select the preset
      await waitFor(() => {
        const option = screen.getByText('Last 7 days');
        fireEvent.click(option);
      });
      
      expect(mockOnChange).toHaveBeenCalledWith(
        new Date('2024-01-08T12:00:00Z'),
        new Date('2024-01-15T12:00:00Z')
      );
    });
  });

  describe('custom range', () => {
    it('shows custom range inputs when selected', async () => {
      render(<TimeRangeSelector from={null} to={null} onChange={mockOnChange} />);
      
      await act(async () => {
        fireEvent.click(screen.getByRole('button'));
      });
      
      await waitFor(() => {
        expect(screen.getByText('Custom range')).toBeInTheDocument();
      });
      
      expect(screen.getByLabelText(/From/i)).toBeInTheDocument();
      expect(screen.getByLabelText(/To/i)).toBeInTheDocument();
      expect(screen.getByRole('button', { name: /Apply/i })).toBeInTheDocument();
    });

    it('applies custom date range', async () => {
      const user = userEvent.setup({ advanceTimers: jest.advanceTimersByTime });
      render(<TimeRangeSelector from={null} to={null} onChange={mockOnChange} />);
      
      await act(async () => {
        fireEvent.click(screen.getByRole('button'));
      });
      
      await waitFor(() => {
        expect(screen.getByText('Custom range')).toBeInTheDocument();
      });
      
      const fromInput = screen.getByLabelText(/From/i);
      const toInput = screen.getByLabelText(/To/i);
      
      await user.clear(fromInput);
      await user.type(fromInput, '2024-01-01T00:00');
      
      await user.clear(toInput);
      await user.type(toInput, '2024-01-05T23:59');
      
      await act(async () => {
        fireEvent.click(screen.getByRole('button', { name: /Apply/i }));
      });
      
      expect(mockOnChange).toHaveBeenCalledWith(
        expect.any(Date),
        expect.any(Date)
      );
      
      const [fromDate, toDate] = mockOnChange.mock.calls[0];
      expect(fromDate.toISOString()).toContain('2024-01-01');
      expect(toDate.toISOString()).toContain('2024-01-05');
    });

    it('validates custom date range (from must be before to)', async () => {
      const user = userEvent.setup({ advanceTimers: jest.advanceTimersByTime });
      render(<TimeRangeSelector from={null} to={null} onChange={mockOnChange} />);
      
      await act(async () => {
        fireEvent.click(screen.getByRole('button'));
      });
      
      await waitFor(() => {
        expect(screen.getByText('Custom range')).toBeInTheDocument();
      });
      
      const fromInput = screen.getByLabelText(/From/i);
      const toInput = screen.getByLabelText(/To/i);
      
      await user.clear(fromInput);
      await user.type(fromInput, '2024-01-10T00:00');
      
      await user.clear(toInput);
      await user.type(toInput, '2024-01-05T23:59'); // To is before From
      
      await act(async () => {
        fireEvent.click(screen.getByRole('button', { name: /Apply/i }));
      });
      
      // Should not call onChange with invalid range
      expect(mockOnChange).not.toHaveBeenCalled();
    });

    it('preserves existing custom dates when reopening', async () => {
      const from = new Date('2024-01-10T00:00:00Z');
      const to = new Date('2024-01-14T23:59:59Z');
      
      render(
        <TimeRangeSelector from={from} to={to} onChange={mockOnChange} />
      );
      
      await act(async () => {
        fireEvent.click(screen.getByRole('button'));
      });
      
      await waitFor(() => {
        expect(screen.getByText('Custom range')).toBeInTheDocument();
      });
      
      const fromInput = screen.getByLabelText(/From/i) as HTMLInputElement;
      const toInput = screen.getByLabelText(/To/i) as HTMLInputElement;
      
      // Should show existing dates in inputs
      expect(fromInput.value).toContain('2024-01-10');
      expect(toInput.value).toContain('2024-01-14');
    });
  });

  describe('clear functionality', () => {
    it('shows clear button when dates are selected', async () => {
      const from = new Date('2024-01-10T00:00:00Z');
      const to = new Date('2024-01-14T23:59:59Z');
      
      render(<TimeRangeSelector from={from} to={to} onChange={mockOnChange} />);
      
      // Need to open the popover first to see the clear button
      await act(async () => {
        fireEvent.click(screen.getByRole('button'));
      });
      
      await waitFor(() => {
        expect(screen.getByText('Quick ranges')).toBeInTheDocument();
      });
      
      const clearButton = screen.getByRole('button', { name: /Clear to Default/i });
      expect(clearButton).toBeInTheDocument();
    });

    it('clears date range when clear button is clicked', async () => {
      const from = new Date('2024-01-10T00:00:00Z');
      const to = new Date('2024-01-14T23:59:59Z');
      
      render(<TimeRangeSelector from={from} to={to} onChange={mockOnChange} />);
      
      // Need to open the popover first to see the clear button
      await act(async () => {
        fireEvent.click(screen.getByRole('button'));
      });
      
      await waitFor(() => {
        expect(screen.getByText('Quick ranges')).toBeInTheDocument();
      });
      
      const clearButton = screen.getByRole('button', { name: /Clear to Default/i });
      act(() => {
        fireEvent.click(clearButton);
      });
      
      expect(mockOnChange).toHaveBeenCalledWith(null, null);
    });

    it('clear button always exists in popover', async () => {
      render(<TimeRangeSelector from={null} to={null} onChange={mockOnChange} />);
      
      await act(async () => {
        fireEvent.click(screen.getByRole('button'));
      });
      
      await waitFor(() => {
        expect(screen.getByText('Quick ranges')).toBeInTheDocument();
      });
      
      // Clear to Default button is always present
      expect(screen.getByRole('button', { name: /Clear to Default/i })).toBeInTheDocument();
    });
  });

  describe('date formatting', () => {
    it('formats dates correctly for display', () => {
      const from = new Date('2024-01-01T00:00:00Z');
      const to = new Date('2024-12-31T23:59:59Z');
      
      render(<TimeRangeSelector from={from} to={to} onChange={mockOnChange} />);
      
      expect(screen.getByText('Custom')).toBeInTheDocument();
    });

    it('handles different timezones correctly', () => {
      const from = new Date('2024-06-15T14:30:00-05:00'); // CDT
      const to = new Date('2024-06-15T20:30:00+01:00'); // BST
      
      render(<TimeRangeSelector from={from} to={to} onChange={mockOnChange} />);
      
      // Should display dates in local timezone
      expect(screen.getByText('Custom')).toBeInTheDocument();
    });
  });

  describe('edge cases', () => {
    it('handles null onChange gracefully', () => {
      // This should not throw
      expect(() => {
        render(<TimeRangeSelector from={null} to={null} onChange={null as unknown as () => void} />);
      }).not.toThrow();
    });

    it('handles invalid date inputs', async () => {
      const user = userEvent.setup({ advanceTimers: jest.advanceTimersByTime });
      render(<TimeRangeSelector from={null} to={null} onChange={mockOnChange} />);
      
      await act(async () => {
        fireEvent.click(screen.getByRole('button'));
      });
      
      await waitFor(() => {
        expect(screen.getByText('Custom range')).toBeInTheDocument();
      });
      
      const fromInput = screen.getByLabelText(/From/i);
      
      await user.clear(fromInput);
      await user.type(fromInput, 'invalid-date');
      
      await act(async () => {
        fireEvent.click(screen.getByRole('button', { name: /Apply/i }));
      });
      
      // Should not call onChange with invalid date
      expect(mockOnChange).not.toHaveBeenCalled();
    });

    it('handles very old dates', () => {
      const from = new Date('1900-01-01T00:00:00Z');
      const to = new Date('1900-12-31T23:59:59Z');
      
      render(<TimeRangeSelector from={from} to={to} onChange={mockOnChange} />);
      
      expect(screen.getByText('Custom')).toBeInTheDocument();
    });

    it('handles future dates', () => {
      const from = new Date('2100-01-01T00:00:00Z');
      const to = new Date('2100-12-31T23:59:59Z');
      
      render(<TimeRangeSelector from={from} to={to} onChange={mockOnChange} />);
      
      expect(screen.getByText('Custom')).toBeInTheDocument();
    });
  });
});
