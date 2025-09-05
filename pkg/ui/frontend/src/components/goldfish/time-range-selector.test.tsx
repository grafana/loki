import { render, screen, fireEvent, waitFor } from '@testing-library/react';
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
      expect(screen.getByText(/Select date range/i)).toBeInTheDocument();
    });

    it('renders with selected dates', () => {
      const from = new Date('2024-01-10T00:00:00Z');
      const to = new Date('2024-01-14T23:59:59Z');
      
      render(<TimeRangeSelector from={from} to={to} onChange={mockOnChange} />);
      
      expect(screen.getByText(/Jan 10, 2024/i)).toBeInTheDocument();
      expect(screen.getByText(/Jan 14, 2024/i)).toBeInTheDocument();
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
        expect(screen.getByText('Last 15 minutes')).toBeInTheDocument();
        expect(screen.getByText('Last 1 hour')).toBeInTheDocument();
        expect(screen.getByText('Last 4 hours')).toBeInTheDocument();
        expect(screen.getByText('Last 24 hours')).toBeInTheDocument();
        expect(screen.getByText('Last 7 days')).toBeInTheDocument();
        expect(screen.getByText('Custom range')).toBeInTheDocument();
      });
    });

    it('selects last 15 minutes preset', async () => {
      render(<TimeRangeSelector from={null} to={null} onChange={mockOnChange} />);
      
      fireEvent.click(screen.getByRole('button'));
      
      await waitFor(() => {
        fireEvent.click(screen.getByText('Last 15 minutes'));
      });
      
      expect(mockOnChange).toHaveBeenCalledWith(
        new Date('2024-01-15T11:45:00Z'),
        new Date('2024-01-15T12:00:00Z')
      );
    });

    it('selects last 4 hours preset', async () => {
      render(<TimeRangeSelector from={null} to={null} onChange={mockOnChange} />);
      
      fireEvent.click(screen.getByRole('button'));
      
      await waitFor(() => {
        fireEvent.click(screen.getByText('Last 4 hours'));
      });
      
      expect(mockOnChange).toHaveBeenCalledWith(
        new Date('2024-01-15T08:00:00Z'),
        new Date('2024-01-15T12:00:00Z')
      );
    });


    it('selects last 7 days preset', async () => {
      render(<TimeRangeSelector from={null} to={null} onChange={mockOnChange} />);
      
      fireEvent.click(screen.getByRole('button'));
      
      await waitFor(() => {
        fireEvent.click(screen.getByText('Last 7 days'));
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
      
      fireEvent.click(screen.getByRole('button'));
      
      await waitFor(() => {
        fireEvent.click(screen.getByText('Custom range'));
      });
      
      expect(screen.getByLabelText(/From/i)).toBeInTheDocument();
      expect(screen.getByLabelText(/To/i)).toBeInTheDocument();
      expect(screen.getByRole('button', { name: /Apply/i })).toBeInTheDocument();
    });

    it('applies custom date range', async () => {
      const user = userEvent.setup({ advanceTimers: jest.advanceTimersByTime });
      render(<TimeRangeSelector from={null} to={null} onChange={mockOnChange} />);
      
      fireEvent.click(screen.getByRole('button'));
      
      await waitFor(() => {
        fireEvent.click(screen.getByText('Custom range'));
      });
      
      const fromInput = screen.getByLabelText(/From/i);
      const toInput = screen.getByLabelText(/To/i);
      
      await user.clear(fromInput);
      await user.type(fromInput, '2024-01-01T00:00');
      
      await user.clear(toInput);
      await user.type(toInput, '2024-01-05T23:59');
      
      fireEvent.click(screen.getByRole('button', { name: /Apply/i }));
      
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
      
      fireEvent.click(screen.getByRole('button'));
      
      await waitFor(() => {
        fireEvent.click(screen.getByText('Custom range'));
      });
      
      const fromInput = screen.getByLabelText(/From/i);
      const toInput = screen.getByLabelText(/To/i);
      
      await user.clear(fromInput);
      await user.type(fromInput, '2024-01-10T00:00');
      
      await user.clear(toInput);
      await user.type(toInput, '2024-01-05T23:59'); // To is before From
      
      fireEvent.click(screen.getByRole('button', { name: /Apply/i }));
      
      // Should not call onChange with invalid range
      expect(mockOnChange).not.toHaveBeenCalled();
    });

    it('preserves existing custom dates when reopening', async () => {
      const from = new Date('2024-01-10T00:00:00Z');
      const to = new Date('2024-01-14T23:59:59Z');
      
      render(
        <TimeRangeSelector from={from} to={to} onChange={mockOnChange} />
      );
      
      fireEvent.click(screen.getByRole('button'));
      
      await waitFor(() => {
        fireEvent.click(screen.getByText('Custom range'));
      });
      
      const fromInput = screen.getByLabelText(/From/i) as HTMLInputElement;
      const toInput = screen.getByLabelText(/To/i) as HTMLInputElement;
      
      // Should show existing dates in inputs
      expect(fromInput.value).toContain('2024-01-10');
      expect(toInput.value).toContain('2024-01-14');
    });
  });

  describe('clear functionality', () => {
    it('shows clear button when dates are selected', () => {
      const from = new Date('2024-01-10T00:00:00Z');
      const to = new Date('2024-01-14T23:59:59Z');
      
      render(<TimeRangeSelector from={from} to={to} onChange={mockOnChange} />);
      
      const clearButton = screen.getByRole('button', { name: /clear/i });
      expect(clearButton).toBeInTheDocument();
    });

    it('clears date range when clear button is clicked', () => {
      const from = new Date('2024-01-10T00:00:00Z');
      const to = new Date('2024-01-14T23:59:59Z');
      
      render(<TimeRangeSelector from={from} to={to} onChange={mockOnChange} />);
      
      const clearButton = screen.getByRole('button', { name: /clear/i });
      fireEvent.click(clearButton);
      
      expect(mockOnChange).toHaveBeenCalledWith(null, null);
    });

    it('does not show clear button when no dates selected', () => {
      render(<TimeRangeSelector from={null} to={null} onChange={mockOnChange} />);
      
      expect(screen.queryByRole('button', { name: /clear/i })).not.toBeInTheDocument();
    });
  });

  describe('date formatting', () => {
    it('formats dates correctly for display', () => {
      const from = new Date('2024-01-01T00:00:00Z');
      const to = new Date('2024-12-31T23:59:59Z');
      
      render(<TimeRangeSelector from={from} to={to} onChange={mockOnChange} />);
      
      expect(screen.getByText(/Jan 1, 2024/i)).toBeInTheDocument();
      expect(screen.getByText(/Dec 31, 2024/i)).toBeInTheDocument();
    });

    it('handles different timezones correctly', () => {
      const from = new Date('2024-06-15T14:30:00-05:00'); // CDT
      const to = new Date('2024-06-15T20:30:00+01:00'); // BST
      
      render(<TimeRangeSelector from={from} to={to} onChange={mockOnChange} />);
      
      // Should display dates in local timezone
      expect(screen.getByText(/Jun 15, 2024/i)).toBeInTheDocument();
    });
  });

  describe('keyboard navigation', () => {
    it('opens dropdown with Enter key', async () => {
      render(<TimeRangeSelector from={null} to={null} onChange={mockOnChange} />);
      
      const button = screen.getByRole('button');
      button.focus();
      fireEvent.keyDown(button, { key: 'Enter' });
      
      await waitFor(() => {
        expect(screen.getByText('Last 15 minutes')).toBeInTheDocument();
      });
    });

    it('closes dropdown with Escape key', async () => {
      render(<TimeRangeSelector from={null} to={null} onChange={mockOnChange} />);
      
      fireEvent.click(screen.getByRole('button'));
      
      await waitFor(() => {
        expect(screen.getByText('Last 15 minutes')).toBeInTheDocument();
      });
      
      fireEvent.keyDown(document, { key: 'Escape' });
      
      await waitFor(() => {
        expect(screen.queryByText('Last 15 minutes')).not.toBeInTheDocument();
      });
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
      
      fireEvent.click(screen.getByRole('button'));
      
      await waitFor(() => {
        fireEvent.click(screen.getByText('Custom range'));
      });
      
      const fromInput = screen.getByLabelText(/From/i);
      
      await user.clear(fromInput);
      await user.type(fromInput, 'invalid-date');
      
      fireEvent.click(screen.getByRole('button', { name: /Apply/i }));
      
      // Should not call onChange with invalid date
      expect(mockOnChange).not.toHaveBeenCalled();
    });

    it('handles very old dates', () => {
      const from = new Date('1900-01-01T00:00:00Z');
      const to = new Date('1900-12-31T23:59:59Z');
      
      render(<TimeRangeSelector from={from} to={to} onChange={mockOnChange} />);
      
      expect(screen.getByText(/Jan 1, 1900/i)).toBeInTheDocument();
      expect(screen.getByText(/Dec 31, 1900/i)).toBeInTheDocument();
    });

    it('handles future dates', () => {
      const from = new Date('2100-01-01T00:00:00Z');
      const to = new Date('2100-12-31T23:59:59Z');
      
      render(<TimeRangeSelector from={from} to={to} onChange={mockOnChange} />);
      
      expect(screen.getByText(/Jan 1, 2100/i)).toBeInTheDocument();
      expect(screen.getByText(/Dec 31, 2100/i)).toBeInTheDocument();
    });
  });
});
